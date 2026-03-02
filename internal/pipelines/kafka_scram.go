package pipelines

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/xdg-go/scram"
)

// SHA256 and SHA512 hash generators for SCRAM authentication.
var (
	SHA256 scram.HashGeneratorFcn = func() hash.Hash { return sha256.New() }
	SHA512 scram.HashGeneratorFcn = func() hash.Hash { return sha512.New() }
)

// scramClient implements sarama.SCRAMClient using xdg-go/scram.
type scramClient struct {
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (c *scramClient) Begin(userName, password, authzID string) (err error) {
	client, err := c.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	c.ClientConversation = client.NewConversation()
	return nil
}

func (c *scramClient) Step(challenge string) (string, error) {
	return c.ClientConversation.Step(challenge)
}

func (c *scramClient) Done() bool {
	return c.ClientConversation.Done()
}
