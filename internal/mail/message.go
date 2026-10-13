package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"
	"time"
)

// Message is one mail: a plain-text body, an optional HTML alternative and
// optional attachments.
type Message struct {
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
}

// Attachment is a file sent with a message.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// cleanHeader removes line breaks, which would start a new header.
func cleanHeader(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
}

func encodeHeader(s string) string {
	s = cleanHeader(s)
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}

// BuildMIME renders a message for SMTP. A text-only message is a single
// text/plain part, as before. With HTML or attachments it is
// multipart/mixed holding a multipart/alternative (text, then HTML) and the
// attachments, base64 encoded.
func BuildMIME(from string, to []string, m Message) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("From: " + cleanHeader(from) + "\r\n")
	b.WriteString("To: " + cleanHeader(strings.Join(to, ", ")) + "\r\n")
	b.WriteString("Subject: " + encodeHeader(m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	if m.HTML == "" && len(m.Attachments) == 0 {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(m.Text)
		return b.Bytes(), nil
	}

	mixed := multipart.NewWriter(&b)
	b.WriteString("Content-Type: multipart/mixed; boundary=" + mixed.Boundary() + "\r\n\r\n")

	var alt bytes.Buffer
	altWriter := multipart.NewWriter(&alt)
	if err := writeQPPart(altWriter, "text/plain; charset=UTF-8", m.Text); err != nil {
		return nil, err
	}
	if m.HTML != "" {
		if err := writeQPPart(altWriter, "text/html; charset=UTF-8", m.HTML); err != nil {
			return nil, err
		}
	}
	if err := altWriter.Close(); err != nil {
		return nil, err
	}
	altHeader := textproto.MIMEHeader{}
	altHeader.Set("Content-Type", "multipart/alternative; boundary="+altWriter.Boundary())
	part, err := mixed.CreatePart(altHeader)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(alt.Bytes()); err != nil {
		return nil, err
	}

	for _, a := range m.Attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		name := cleanHeader(a.Filename)
		if name == "" {
			name = "attachment"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", mime.FormatMediaType(ct, map[string]string{"name": name}))
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		h.Set("Content-Transfer-Encoding", "base64")
		w, err := mixed.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if err := writeBase64Lines(w, a.Data); err != nil {
			return nil, err
		}
	}
	if err := mixed.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeQPPart(w *multipart.Writer, contentType, body string) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Type", contentType)
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
}

// writeBase64Lines writes data as base64 in 76 character lines (RFC 2045).
func writeBase64Lines(w interface{ Write([]byte) (int, error) }, data []byte) error {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		if _, err := fmt.Fprintf(w, "%s\r\n", enc[:76]); err != nil {
			return err
		}
		enc = enc[76:]
	}
	if enc != "" {
		if _, err := fmt.Fprintf(w, "%s\r\n", enc); err != nil {
			return err
		}
	}
	return nil
}
