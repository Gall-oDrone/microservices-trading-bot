package datahealth

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RunBook is what one run logged for a book.
type RunBook struct {
	Book   string `json:"book"`
	Stage  string `json:"stage"`  // the last "stage: …" line
	Ledger string `json:"ledger"` // the last "ledger: …" outcome
}

// RunLog is one daily-executor run log (run-<UTC>.log next to the ledger),
// as written by scripts/daily-executor-run.sh: the executor's output, then
// "upload=ok|failed <dest>" when uploading, then "exit=<code>".
type RunLog struct {
	File         string    `json:"file"`
	StartedAt    string    `json:"started_at"`
	ModifiedAt   string    `json:"modified_at"`
	Version      string    `json:"version"`
	Mode         string    `json:"mode"`
	ExitCode     *int      `json:"exit_code"`
	Status       string    `json:"status"`
	Message      string    `json:"message"`
	Upload       string    `json:"upload"` // ok | failed | "" (not uploaded)
	UploadTarget string    `json:"upload_target"`
	Books        []RunBook `json:"books"`
	Errors       []string  `json:"errors"`
}

// RunningFor is how long a log without an exit line counts as a run still in
// progress: the maker order may rest an hour before the market fallback.
const RunningFor = 90 * time.Minute

var (
	runFileRe = regexp.MustCompile(`^run-(\d{8}T\d{6}Z)\.log$`)
	bookLine  = regexp.MustCompile(`^\[([a-z]{2,6}_[a-z]{2,6})\] (?:\S+ )?(stage|ledger): (.*)$`)
	sumLedger = regexp.MustCompile(`^\s+ledger\s+: (.*)$`)
	bookHead  = regexp.MustCompile(`^\[([a-z]{2,6}_[a-z]{2,6})\] \S+\.md$`)
	exitLine  = regexp.MustCompile(`^exit=(-?\d+)$`)
	uploadRe  = regexp.MustCompile(`^upload=(ok|failed)(?: (.*))?$`)
	errLine   = regexp.MustCompile(`(?i)\b(error|refus|panic|fatal|blocked)`)
)

// IsRunLog reports whether name is a run-<UTC>.log file name.
func IsRunLog(name string) bool { return runFileRe.MatchString(name) }

// ReadRuns returns the newest n run logs in dir, newest first. A missing dir
// is not an error.
func ReadRuns(dir string, n int, now time.Time) ([]RunLog, error) {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []RunLog{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && runFileRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	// The UTC stamp sorts lexically.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) > n {
		names = names[:n]
	}
	out := make([]RunLog, 0, len(names))
	for _, name := range names {
		r, err := ParseRunLog(filepath.Join(dir, name), now)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ParseRunLog reads one run log.
func ParseRunLog(p string, now time.Time) (RunLog, error) {
	name := filepath.Base(p)
	f, err := os.Open(p)
	if err != nil {
		return RunLog{File: name, Books: []RunBook{}, Errors: []string{}}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return RunLog{File: name, Books: []RunBook{}, Errors: []string{}}, err
	}
	return ParseRun(name, fi.ModTime(), f, now)
}

// ParseRun parses one run log's content; name is its file name and modified
// its last write (the S3 copy's LastModified is the upload time, just after
// the exit line).
func ParseRun(name string, modified time.Time, body io.Reader, now time.Time) (RunLog, error) {
	r := RunLog{File: name, Books: []RunBook{}, Errors: []string{}}
	if m := runFileRe.FindStringSubmatch(name); m != nil {
		if t, err := time.Parse("20060102T150405Z", m[1]); err == nil {
			r.StartedAt = t.UTC().Format(time.RFC3339)
		}
	}
	r.ModifiedAt = modified.UTC().Format(time.RFC3339)

	books := map[string]*RunBook{}
	book := func(b string) *RunBook {
		if books[b] == nil {
			books[b] = &RunBook{Book: b}
		}
		return books[b]
	}
	current := ""
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	first := true
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if first {
			first = false
			parseHeader(&r, line)
		}
		switch {
		case exitLine.MatchString(line):
			code, _ := strconv.Atoi(exitLine.FindStringSubmatch(line)[1])
			r.ExitCode = &code
		case uploadRe.MatchString(line):
			m := uploadRe.FindStringSubmatch(line)
			r.Upload, r.UploadTarget = m[1], m[2]
		case bookHead.MatchString(line):
			current = bookHead.FindStringSubmatch(line)[1]
			book(current)
		case bookLine.MatchString(line):
			m := bookLine.FindStringSubmatch(line)
			b := book(m[1])
			if m[2] == "stage" {
				b.Stage = m[3]
			} else {
				b.Ledger = m[3]
			}
		case sumLedger.MatchString(line) && current != "":
			book(current).Ledger = sumLedger.FindStringSubmatch(line)[1]
		}
		if errLine.MatchString(line) && len(r.Errors) < 5 {
			r.Errors = append(r.Errors, strings.TrimSpace(line))
		}
	}
	if err := sc.Err(); err != nil {
		return r, fmt.Errorf("%s: %w", name, err)
	}
	keys := make([]string, 0, len(books))
	for k := range books {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r.Books = append(r.Books, *books[k])
	}

	switch {
	case r.ExitCode != nil && *r.ExitCode == 0:
		r.Status, r.Message = OK, "exit 0"
	case r.ExitCode != nil && *r.ExitCode == 2:
		r.Status, r.Message = Fail, "exit 2: refused to run (bad flags, policy or halt file); nothing was recorded"
	case r.ExitCode != nil:
		r.Status, r.Message = Fail, fmt.Sprintf("exit %d: a book failed or an order was blocked; read the log", *r.ExitCode)
	case now.Sub(modified) < RunningFor:
		r.Status, r.Message = Unknown, "no exit code yet: still running?"
	default:
		r.Status, r.Message = Warn, "no exit code recorded (run without scripts/daily-executor-run.sh, or it crashed)"
	}
	if r.Upload == "failed" && r.Status == OK {
		r.Status, r.Message = Warn, "exit 0, but the S3 upload failed"
	}
	return r, nil
}

// parseHeader reads "daily-executor <version> | as of … | mode <mode> | …".
func parseHeader(r *RunLog, line string) {
	if !strings.HasPrefix(line, "daily-executor ") {
		return
	}
	parts := strings.Split(line, " | ")
	if f := strings.Fields(parts[0]); len(f) >= 2 {
		r.Version = f[1]
	}
	for _, p := range parts[1:] {
		if v, ok := strings.CutPrefix(p, "mode "); ok {
			r.Mode = strings.TrimSpace(v)
		}
	}
}
