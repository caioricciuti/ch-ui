package pipelines

import "fmt"

// NewSource returns a SourceConnector for the given node type.
func NewSource(nodeType string) (SourceConnector, error) {
	switch nodeType {
	case "source_kafka":
		return &KafkaSource{}, nil
	case "source_webhook":
		return &WebhookSource{}, nil
	case "source_database":
		return &DatabaseSource{}, nil
	case "source_s3":
		return &S3Source{}, nil
	default:
		return nil, fmt.Errorf("unknown source type: %s", nodeType)
	}
}
