package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

func TestResendPayloadWithAttachment(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emails" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"re_123"}`))
	}))
	defer srv.Close()
	cfg := map[string]interface{}{"api_key": "key", "from_email": "r@example.com", "base_url": srv.URL}
	id, err := (&Sender{}).SendMessage(context.Background(), ChannelTypeResend, cfg, []string{"a@example.com"}, testMessage())
	if err != nil || id != "re_123" {
		t.Fatalf("send: %q %v", id, err)
	}
	if strings.ContainsAny(got["subject"].(string), "\r\n") {
		t.Fatal("subject keeps a line break")
	}
	if got["html"] != "<p>Hello</p>" || !strings.HasPrefix(got["text"].(string), "Hello") {
		t.Fatalf("bodies %v", got)
	}
	atts, _ := got["attachments"].([]interface{})
	if len(atts) != 1 {
		t.Fatalf("attachments %v", got["attachments"])
	}
	a := atts[0].(map[string]interface{})
	data, _ := base64.StdEncoding.DecodeString(a["content"].(string))
	if a["filename"] != "report.csv" || string(data) != "a,b\r\n1,2\r\n" {
		t.Fatalf("attachment %v", a)
	}
}

func TestBrevoPayloadWithAttachment(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/smtp/email" || r.Header.Get("api-key") != "key" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"messageId":"<b@1>"}`))
	}))
	defer srv.Close()
	cfg := map[string]interface{}{"api_key": "key", "from_email": "r@example.com", "base_url": srv.URL}
	id, err := (&Sender{}).SendMessage(context.Background(), ChannelTypeBrevo, cfg, []string{"a@example.com"}, testMessage())
	if err != nil || id != "<b@1>" {
		t.Fatalf("send: %q %v", id, err)
	}
	if got["htmlContent"] != "<p>Hello</p>" || got["textContent"] == nil {
		t.Fatalf("bodies %v", got)
	}
	atts, _ := got["attachment"].([]interface{})
	if len(atts) != 1 {
		t.Fatalf("attachment %v", got["attachment"])
	}
	a := atts[0].(map[string]interface{})
	data, _ := base64.StdEncoding.DecodeString(a["content"].(string))
	if a["name"] != "report.csv" || string(data) != "a,b\r\n1,2\r\n" {
		t.Fatalf("attachment %v", a)
	}

	// A plain Send still carries no HTML or attachment keys.
	got = nil
	if _, err := Send(context.Background(), ChannelTypeBrevo, cfg, []string{"a@example.com"}, "s", "body"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["attachment"]; ok {
		t.Fatal("plain send added an attachment")
	}
	if _, ok := got["htmlContent"]; ok {
		t.Fatal("plain send added HTML")
	}
}
