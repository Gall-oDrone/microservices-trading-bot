package store

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/ui-api/internal/objstore"
)

// Info describes one file of a ledger's directory.
type Info struct {
	Path    string // as passed to FS.ReadFile
	Name    string // base name
	Size    int64
	ModTime time.Time
	// Tag changes whenever the content may have changed (size+mtime on disk,
	// the ETag in S3); caches compare it.
	Tag string
}

// FS is where a ledger's files live: the local disk or an S3 prefix
// (copied there by scripts/daily-executor-run.sh). It is read-only.
type FS interface {
	// Stat returns an error wrapping fs.ErrNotExist for a missing file.
	Stat(p string) (Info, error)
	ReadFile(p string) ([]byte, error)
	// Files lists the files directly in dir whose name has the given prefix
	// and suffix, sorted by name.
	Files(dir, prefix, suffix string) ([]Info, error)
	// Join and Dir are path.Join/filepath.Join and Dir for this FS.
	Join(elem ...string) string
	Dir(p string) string
	// URI is p for display: a path on disk, s3://bucket/key in S3.
	URI(p string) string
}

// osFS is the local disk.
type osFS struct{}

func (osFS) Stat(p string) (Info, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return Info{}, err
	}
	return osInfo(p, fi), nil
}

func osInfo(p string, fi os.FileInfo) Info {
	return Info{Path: p, Name: fi.Name(), Size: fi.Size(), ModTime: fi.ModTime(),
		Tag: fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().UnixNano())}
}

func (osFS) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

func (osFS) Files(dir, prefix, suffix string) ([]Info, error) {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, suffix) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		out = append(out, osInfo(filepath.Join(dir, n), fi))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (osFS) Join(elem ...string) string { return filepath.Join(elem...) }
func (osFS) Dir(p string) string        { return filepath.Dir(p) }
func (osFS) URI(p string) string        { return p }

// S3 listing and download limits. The executor writes once a day, so a
// listing at most every ListTTL keeps the pages current without listing on
// every request.
const (
	ListTTL      = 30 * time.Second
	listErrTTL   = 5 * time.Second
	listDeadline = 10 * time.Second
	getDeadline  = 20 * time.Second
)

// remoteFS reads one S3 prefix through objstore: one cached listing of the
// whole prefix, and bodies cached by ETag.
type remoteFS struct {
	obj  objstore.Store
	root string // prefix without a trailing slash
	now  func() time.Time

	mu       sync.Mutex
	listedAt time.Time
	listErr  error
	objects  map[string]objstore.Object
	bodies   map[string]body
}

type body struct {
	etag string
	b    []byte
}

func newRemoteFS(obj objstore.Store, root string) *remoteFS {
	return &remoteFS{obj: obj, root: strings.Trim(root, "/"), now: time.Now,
		objects: map[string]objstore.Object{}, bodies: map[string]body{}}
}

// list refreshes the listing when it is older than ListTTL (listErrTTL
// after a failure). Callers hold r.mu.
func (r *remoteFS) list() error {
	now := r.now()
	ttl := ListTTL
	if r.listErr != nil {
		ttl = listErrTTL
	}
	if !r.listedAt.IsZero() && now.Sub(r.listedAt) < ttl {
		return r.listErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), listDeadline)
	defer cancel()
	prefix := r.root + "/"
	if r.root == "" {
		prefix = ""
	}
	objs, err := r.obj.List(ctx, prefix)
	r.listedAt, r.listErr = now, err
	if err != nil {
		return fmt.Errorf("list %s: %w", r.URI(r.root), err)
	}
	r.objects = make(map[string]objstore.Object, len(objs))
	for _, o := range objs {
		r.objects[o.Key] = o
	}
	for k := range r.bodies { // drop bodies of deleted keys
		if _, ok := r.objects[k]; !ok {
			delete(r.bodies, k)
		}
	}
	return nil
}

func remoteInfo(o objstore.Object) Info {
	return Info{Path: o.Key, Name: path.Base(o.Key), Size: o.Size, ModTime: o.LastModified, Tag: o.ETag}
}

func (r *remoteFS) Stat(p string) (Info, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.list(); err != nil {
		return Info{}, err
	}
	o, ok := r.objects[p]
	if !ok {
		return Info{}, &fs.PathError{Op: "stat", Path: r.URI(p), Err: fs.ErrNotExist}
	}
	return remoteInfo(o), nil
}

func (r *remoteFS) ReadFile(p string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.list(); err != nil {
		return nil, err
	}
	o, ok := r.objects[p]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: r.URI(p), Err: fs.ErrNotExist}
	}
	if c, ok := r.bodies[p]; ok && c.etag == o.ETag {
		return c.b, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), getDeadline)
	defer cancel()
	b, err := r.obj.Get(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", r.URI(p), err)
	}
	r.bodies[p] = body{etag: o.ETag, b: b}
	return b, nil
}

func (r *remoteFS) Files(dir, prefix, suffix string) ([]Info, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.list(); err != nil {
		return nil, err
	}
	var out []Info
	for k, o := range r.objects {
		if path.Dir(k) != dir {
			continue
		}
		if n := path.Base(k); strings.HasPrefix(n, prefix) && strings.HasSuffix(n, suffix) {
			out = append(out, remoteInfo(o))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *remoteFS) Join(elem ...string) string { return path.Join(elem...) }
func (r *remoteFS) Dir(p string) string        { return path.Dir(p) }
func (r *remoteFS) URI(p string) string        { return strings.TrimSuffix(r.obj.Describe(), "/") + "/" + p }
