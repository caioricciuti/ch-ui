package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"strings"
	"testing"
)

func testMessage() Message {
	return Message{
		Subject: "Report: ventes été\r\nBcc: evil@example.com",
		Text:    "Hello\n" + strings.Repeat("long line ", 200),
		HTML:    "<p>Hello</p>",
		Attachments: []Attachment{{
			Filename: "report.csv", ContentType: "text/csv; charset=utf-8", Data: []byte("a,b\r\n1,2\r\n"),
		}},
	}
}

// TestBuildMIMEStructure parses the message back: a multipart/mixed whose
// first part is multipart/alternative (text, HTML) and whose second is the
// CSV attachment. Line breaks in the subject cannot start a header.
func TestBuildMIMEStructure(t *testing.T) {
	m := testMessage()
	raw, err := BuildMIME("CH-UI <reports@example.com>", []string{"a@example.com", "b@example.com"}, m)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("subject injected a header")
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || !strings.Contains(subject, "ventes été") {
		t.Fatalf("subject %q %v", subject, err)
	}
	to, err := msg.Header.AddressList("To")
	if err != nil || len(to) != 2 {
		t.Fatalf("to: %v %v", to, err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("top type %q %v", mediaType, err)
	}
	mixed := multipart.NewReader(msg.Body, params["boundary"])

	alt, err := mixed.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	altType, altParams, err := mime.ParseMediaType(alt.Header.Get("Content-Type"))
	if err != nil || altType != "multipart/alternative" {
		t.Fatalf("first part %q %v", altType, err)
	}
	inner := multipart.NewReader(alt, altParams["boundary"])
	var bodies []string
	var types []string
	for {
		p, err := inner.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, strings.Split(p.Header.Get("Content-Type"), ";")[0])
		// multipart.Reader decodes quoted-printable itself.
		b, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(b))
	}
	if strings.Join(types, ",") != "text/plain,text/html" {
		t.Fatalf("alternative parts %v", types)
	}
	// Text parts travel with CRLF line ends.
	if strings.ReplaceAll(bodies[0], "\r\n", "\n") != m.Text || bodies[1] != m.HTML {
		t.Fatal("bodies did not round trip")
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatal("a line is longer than SMTP allows")
		}
	}

	att, err := mixed.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if att.FileName() != "report.csv" {
		t.Fatalf("attachment name %q", att.FileName())
	}
	if att.Header.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatal("attachment must be base64")
	}
	enc, _ := io.ReadAll(att)
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(enc), "\r\n", ""))
	if err != nil || string(data) != "a,b\r\n1,2\r\n" {
		t.Fatalf("attachment %q %v", data, err)
	}
	if _, err := mixed.NextPart(); err != io.EOF {
		t.Fatalf("unexpected extra part: %v", err)
	}
}

func TestBuildMIMEPlainTextUnchanged(t *testing.T) {
	raw, err := BuildMIME("x@example.com", []string{"a@example.com"}, Message{Subject: "s", Text: "body"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ct := msg.Header.Get("Content-Type"); ct != "text/plain; charset=UTF-8" {
		t.Fatalf("content type %q", ct)
	}
	b, _ := io.ReadAll(msg.Body)
	if string(b) != "body" {
		t.Fatalf("body %q", b)
	}
}

// Channels of a removed provider fail with an error that says what to do,
// and an unknown type still reads as unsupported.
func TestSendRejectsRemovedProviders(t *testing.T) {
	cfg := map[string]interface{}{"api_key": "key", "from_email": "r@example.com"}
	for _, kind := range []string{"resend", "Brevo"} {
		_, err := Send(context.Background(), kind, cfg, []string{"a@example.com"}, "s", "body")
		if err == nil || !strings.Contains(err.Error(), "no longer supported") || !strings.Contains(err.Error(), "SMTP or webhook") {
			t.Fatalf("%s: err %v", kind, err)
		}
	}
	if _, err := Send(context.Background(), "pigeon", cfg, []string{"a@example.com"}, "s", "body"); err == nil || !strings.Contains(err.Error(), "unsupported channel type: pigeon") {
		t.Fatalf("unknown type: err %v", err)
	}
	if !RemovedChannelType(" BREVO ") || RemovedChannelType(ChannelTypeSMTP) || RemovedChannelType("webhook") {
		t.Fatal("RemovedChannelType misclassifies")
	}
}
