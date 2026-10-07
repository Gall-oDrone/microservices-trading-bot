package alerts

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// Notifier delivers one message.
type Notifier interface {
	Notify(ctx context.Context, subject, body string) error
	Describe() string
}

// Writer prints the message (dry runs, logs).
type Writer struct{ W io.Writer }

// Notify implements Notifier.
func (w Writer) Notify(_ context.Context, subject, body string) error {
	_, err := fmt.Fprintf(w.W, "Subject: %s\n\n%s", subject, body)
	return err
}

// Describe implements Notifier.
func (Writer) Describe() string { return "stdout" }

// snsPublisher is the part of the SNS client used.
type snsPublisher interface {
	Publish(ctx context.Context, in *sns.PublishInput, opts ...func(*sns.Options)) (*sns.PublishOutput, error)
	ListSubscriptionsByTopic(ctx context.Context, in *sns.ListSubscriptionsByTopicInput, opts ...func(*sns.Options)) (*sns.ListSubscriptionsByTopicOutput, error)
}

// SNS publishes to a topic; email and SMS subscribers get the message.
type SNS struct {
	TopicARN string
	client   snsPublisher
}

// NewSNS uses the default AWS credentials; the region comes from the ARN.
func NewSNS(ctx context.Context, topicARN string) (*SNS, error) {
	parts := strings.Split(topicARN, ":")
	if len(parts) != 6 || parts[0] != "arn" || parts[2] != "sns" || parts[3] == "" {
		return nil, fmt.Errorf("%q is not an SNS topic ARN (arn:aws:sns:<region>:<account>:<name>)", topicARN)
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(parts[3]))
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &SNS{TopicARN: topicARN, client: sns.NewFromConfig(cfg)}, nil
}

// Notify implements Notifier. SNS drops messages for subscriptions that are
// not confirmed yet, so a topic without a confirmed one is an error: the
// caller keeps its state and retries, and nothing is silently lost.
func (s *SNS) Notify(ctx context.Context, subject, body string) error {
	if err := s.confirmed(ctx); err != nil {
		return err
	}
	_, err := s.client.Publish(ctx, &sns.PublishInput{TopicArn: aws.String(s.TopicARN),
		Subject: aws.String(asciiLine(subject)), Message: aws.String(body)})
	if err != nil {
		return fmt.Errorf("sns publish %s: %w", s.TopicARN, err)
	}
	return nil
}

func (s *SNS) confirmed(ctx context.Context) error {
	in := &sns.ListSubscriptionsByTopicInput{TopicArn: aws.String(s.TopicARN)}
	pending := 0
	for {
		out, err := s.client.ListSubscriptionsByTopic(ctx, in)
		if err != nil {
			return fmt.Errorf("sns subscriptions %s: %w", s.TopicARN, err)
		}
		for _, sub := range out.Subscriptions {
			switch arn := aws.ToString(sub.SubscriptionArn); arn {
			case "PendingConfirmation":
				pending++
			case "", "Deleted":
			default:
				return nil
			}
		}
		if aws.ToString(out.NextToken) == "" {
			break
		}
		in.NextToken = out.NextToken
	}
	return fmt.Errorf("sns %s has no confirmed subscription (%d pending: click the link in the confirmation email)", s.TopicARN, pending)
}

// Describe implements Notifier.
func (s *SNS) Describe() string { return "sns " + s.TopicARN }
