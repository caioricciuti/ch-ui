package pipelines

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/IBM/sarama"
)

// KafkaSource consumes messages from a Kafka topic using a consumer group.
type KafkaSource struct{}

func (k *KafkaSource) Type() string { return "source_kafka" }

// Validate checks Kafka configuration.
func (k *KafkaSource) Validate(cfg ConnectorConfig) error {
	brokers, _ := cfg.Fields["brokers"].(string)
	topic, _ := cfg.Fields["topic"].(string)
	if brokers == "" {
		return fmt.Errorf("brokers is required")
	}
	if topic == "" {
		return fmt.Errorf("topic is required")
	}
	return nil
}

// Start begins consuming messages from Kafka and sends batches to the output channel.
func (k *KafkaSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	brokers := strings.Split(stringField(cfg.Fields, "brokers", ""), ",")
	topic := stringField(cfg.Fields, "topic", "")
	group := stringField(cfg.Fields, "consumer_group", "ch-ui-pipeline")
	batchSize := intField(cfg.Fields, "batch_size", 500)
	batchTimeoutMs := intField(cfg.Fields, "batch_timeout_ms", 5000)

	config := sarama.NewConfig()
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Version = sarama.V2_6_0_0

	// SASL configuration
	saslMechanism := stringField(cfg.Fields, "sasl_mechanism", "")
	if saslMechanism != "" {
		config.Net.SASL.Enable = true
		config.Net.SASL.User = stringField(cfg.Fields, "sasl_username", "")
		config.Net.SASL.Password = stringField(cfg.Fields, "sasl_password", "")

		switch strings.ToUpper(saslMechanism) {
		case "PLAIN":
			config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		case "SCRAM-SHA-256":
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &scramClient{HashGeneratorFcn: SHA256} }
		case "SCRAM-SHA-512":
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &scramClient{HashGeneratorFcn: SHA512} }
		}
	}

	// TLS
	if boolField(cfg.Fields, "use_tls", false) {
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	// Create consumer group
	client, err := sarama.NewConsumerGroup(brokers, group, config)
	if err != nil {
		return fmt.Errorf("create kafka consumer group: %w", err)
	}
	defer client.Close()

	handler := &kafkaGroupHandler{
		batchSize:      batchSize,
		batchTimeoutMs: batchTimeoutMs,
		out:            out,
	}

	slog.Info("Kafka source started", "brokers", brokers, "topic", topic, "group", group)

	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := client.Consume(ctx, []string{topic}, handler); err != nil {
			return fmt.Errorf("kafka consume: %w", err)
		}
	}
}

// kafkaGroupHandler implements sarama.ConsumerGroupHandler.
type kafkaGroupHandler struct {
	batchSize      int
	batchTimeoutMs int
	out            chan<- Batch
}

func (h *kafkaGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *kafkaGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	var buf []Record
	ticker := time.NewTicker(time.Duration(h.batchTimeoutMs) * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		batch := Batch{
			Records:  buf,
			SourceTS: time.Now(),
		}
		select {
		case h.out <- batch:
		case <-session.Context().Done():
			return
		}
		buf = nil
	}

	for {
		select {
		case <-session.Context().Done():
			flush()
			return nil
		case msg, ok := <-claim.Messages():
			if !ok {
				flush()
				return nil
			}
			var data map[string]interface{}
			if err := json.Unmarshal(msg.Value, &data); err != nil {
				slog.Warn("Kafka message parse error, wrapping as raw", "error", err, "offset", msg.Offset)
				data = map[string]interface{}{
					"_raw":       string(msg.Value),
					"_topic":     msg.Topic,
					"_partition": msg.Partition,
					"_offset":    msg.Offset,
					"_timestamp": msg.Timestamp.UTC().Format(time.RFC3339),
				}
			}
			buf = append(buf, Record{
				Data:    data,
				RawJSON: msg.Value,
			})
			session.MarkMessage(msg, "")
			if len(buf) >= h.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
