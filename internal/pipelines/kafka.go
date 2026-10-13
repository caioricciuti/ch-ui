package pipelines

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
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
	return validateCommon(cfg.Fields)
}

// Start begins consuming messages from Kafka and sends batches to the output channel.
func (k *KafkaSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	brokers := strings.Split(stringField(cfg.Fields, "brokers", ""), ",")
	topic := stringField(cfg.Fields, "topic", "")
	// Existing pipelines saved without a group keep the historical default;
	// the editor fills in ch-ui-<pipeline id> for new ones.
	group := stringField(cfg.Fields, "consumer_group", "")
	if strings.TrimSpace(group) == "" {
		group = "ch-ui-pipeline"
	}
	batchSize := max(intField(cfg.Fields, "batch_size", 1000), 1)
	batchTimeoutMs := max(intField(cfg.Fields, "batch_timeout_ms", 5000), 100)
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}

	config := kafkaConfig(cfg)

	handler := &kafkaGroupHandler{
		batchSize:      batchSize,
		batchTimeoutMs: batchTimeoutMs,
		out:            out,
		health:         cfg.Health,
	}

	slog.Info("Kafka source started", "brokers", brokers, "topic", topic, "group", group)

	// Connect and consume, reconnecting with backoff after a broker error
	// instead of ending the run. Each client is closed before reconnecting
	// and before Start returns.
	backoff := minBackoff
	for ctx.Err() == nil {
		err := consumeOnce(ctx, brokers, group, topic, config, handler)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			cfg.Health.Fail("Kafka source", err)
		}
		if !sleepCtx(ctx, backoff) {
			return nil
		}
		if err != nil {
			backoff = nextBackoff(backoff)
		} else {
			backoff = minBackoff
		}
	}
	return nil
}

// kafkaConfig builds the sarama config (SASL, TLS) for a source config.
func kafkaConfig(cfg ConnectorConfig) *sarama.Config {
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
	return config
}

// consumeOnce opens a consumer group and consumes until ctx is cancelled or
// the group returns an error. The client is always closed on return.
func consumeOnce(ctx context.Context, brokers []string, group, topic string, config *sarama.Config, handler sarama.ConsumerGroupHandler) error {
	client, err := sarama.NewConsumerGroup(brokers, group, config)
	if err != nil {
		return fmt.Errorf("create kafka consumer group: %w", err)
	}
	defer client.Close()
	for {
		if ctx.Err() != nil {
			return nil
		}
		// Consume returns nil on a rebalance; loop to rejoin.
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
	health         *Health
}

// Setup runs once a group session is established: the connection works.
func (h *kafkaGroupHandler) Setup(_ sarama.ConsumerGroupSession) error {
	h.health.OK()
	return nil
}
func (h *kafkaGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	var buf []Record
	var msgs []*sarama.ConsumerMessage
	ticker := time.NewTicker(time.Duration(h.batchTimeoutMs) * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		// Capture the messages this batch is responsible for. Offsets are only
		// marked (and later auto-committed) once the runner acks a durable write,
		// so a crash before the sink write re-delivers these messages.
		marked := msgs
		first, last := marked[0], marked[len(marked)-1]
		batch := Batch{
			Records:  buf,
			SourceTS: time.Now(),
			Ack: func() {
				for _, m := range marked {
					session.MarkMessage(m, "")
				}
			},
			DedupToken: dedupToken("kafka", first.Topic, first.Partition, first.Offset, last.Offset),
			// For display only: Kafka resumes from the group's committed
			// offsets, not from this checkpoint.
			Checkpoint: &Checkpoint{Key: fmt.Sprintf("kafka:%s/%d", last.Topic, last.Partition), Position: strconv.FormatInt(last.Offset+1, 10)},
		}
		select {
		case h.out <- batch:
		case <-session.Context().Done():
			return
		}
		buf = nil
		msgs = nil
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
			buf = append(buf, kafkaRecord(msg))
			msgs = append(msgs, msg)
			if len(buf) >= h.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// kafkaRecord turns a message into a record. A value that is not a JSON
// object is wrapped as {"_raw": ...} with its position, and RawJSON is the
// encoded wrapper so the sink never inserts invalid JSON.
func kafkaRecord(msg *sarama.ConsumerMessage) Record {
	var data map[string]interface{}
	pos := fmt.Sprintf("%s/%d@%d", msg.Topic, msg.Partition, msg.Offset)
	if err := json.Unmarshal(msg.Value, &data); err == nil && data != nil {
		return Record{Data: data, RawJSON: msg.Value, Position: pos}
	}
	slog.Warn("Kafka message is not a JSON object, wrapping as raw", "offset", msg.Offset)
	data = map[string]interface{}{
		"_raw":       string(msg.Value),
		"_topic":     msg.Topic,
		"_partition": msg.Partition,
		"_offset":    msg.Offset,
		"_timestamp": msg.Timestamp.UTC().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(data)
	return Record{Data: data, RawJSON: raw, Position: pos}
}

// Sample reads up to limit of the most recent messages without a consumer
// group, so no offset is ever committed. Each partition is read from its
// newest offset minus its share of limit.
func (k *KafkaSource) Sample(ctx context.Context, cfg ConnectorConfig, limit int) (SampleResult, error) {
	brokers := strings.Split(stringField(cfg.Fields, "brokers", ""), ",")
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}
	topic := stringField(cfg.Fields, "topic", "")
	config := kafkaConfig(cfg)
	config.Consumer.Return.Errors = false
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d > 0 {
			config.Net.DialTimeout = d
			config.Metadata.Timeout = d
		}
	}

	type result struct {
		res SampleResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := kafkaSample(ctx, brokers, topic, config, limit)
		done <- result{res, err}
	}()
	select {
	case r := <-done:
		return r.res, r.err
	case <-ctx.Done():
		return SampleResult{}, fmt.Errorf("no answer from Kafka within the time limit: %w", ctx.Err())
	}
}

func kafkaSample(ctx context.Context, brokers []string, topic string, config *sarama.Config, limit int) (SampleResult, error) {
	client, err := sarama.NewClient(brokers, config)
	if err != nil {
		return SampleResult{}, fmt.Errorf("connect to Kafka: %w", err)
	}
	defer client.Close()
	partitions, err := client.Partitions(topic)
	if err != nil {
		return SampleResult{}, fmt.Errorf("topic %s: %w", topic, err)
	}
	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		return SampleResult{}, err
	}
	defer consumer.Close()

	per := max(limit/max(len(partitions), 1), 1)
	var out []map[string]interface{}
	for _, p := range partitions {
		if len(out) >= limit || ctx.Err() != nil {
			break
		}
		newest, err := client.GetOffset(topic, p, sarama.OffsetNewest)
		if err != nil {
			return SampleResult{}, fmt.Errorf("partition %d: %w", p, err)
		}
		oldest, err := client.GetOffset(topic, p, sarama.OffsetOldest)
		if err != nil {
			return SampleResult{}, fmt.Errorf("partition %d: %w", p, err)
		}
		if newest <= oldest {
			continue
		}
		start := max(newest-int64(per), oldest)
		pc, err := consumer.ConsumePartition(topic, p, start)
		if err != nil {
			return SampleResult{}, fmt.Errorf("partition %d: %w", p, err)
		}
		want := int(newest - start)
	read:
		for want > 0 && len(out) < limit {
			select {
			case msg := <-pc.Messages():
				out = append(out, kafkaRecord(msg).Data)
				want--
			case <-ctx.Done():
				break read
			}
		}
		pc.Close()
	}
	note := fmt.Sprintf("Latest %d messages of %s, read without a consumer group (nothing committed).", len(out), topic)
	if len(out) == 0 {
		note = fmt.Sprintf("Topic %s has no messages yet.", topic)
	}
	return SampleResult{Records: out, Note: note}, nil
}
