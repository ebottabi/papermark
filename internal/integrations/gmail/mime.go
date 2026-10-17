/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package gmail

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"path/filepath"
	"strings"
)

// KeenHeader marks mail Keen sent, so a mailbox trigger never reacts to it.
const KeenHeader = "X-Keen-Sent"

// File is an attachment to send.
type File struct {
	Name string
	Data []byte
}

// Draft is an outgoing message. From is left out: Gmail sets it.
type Draft struct {
	To, Cc, Bcc     []string
	Subject, Body   string
	InReplyTo, Refs string
	Attachments     []File
}

// BuildMessage renders d as an RFC 5322 message with CRLF line endings: a
// quoted-printable UTF-8 text part, wrapped in multipart/mixed when there are attachments.
func BuildMessage(d Draft) ([]byte, error) {
	if strings.ContainsAny(d.Subject+d.InReplyTo+d.Refs, "\r\n") {
		return nil, errors.New("subject and reference headers must be a single line")
	}
	if len(d.To)+len(d.Cc)+len(d.Bcc) == 0 {
		return nil, errors.New("at least one recipient is required")
	}
	var buf bytes.Buffer
	h := func(name, value string) { buf.WriteString(name + ": " + value + "\r\n") }
	for _, f := range []struct {
		name string
		list []string
	}{{"To", d.To}, {"Cc", d.Cc}, {"Bcc", d.Bcc}} {
		if len(f.list) == 0 {
			continue
		}
		v, err := addressHeader(f.list)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", strings.ToLower(f.name), err)
		}
		h(f.name, v)
	}
	h("Subject", mime.QEncoding.Encode("utf-8", d.Subject))
	if d.InReplyTo != "" {
		h("In-Reply-To", d.InReplyTo)
	}
	if d.Refs != "" {
		h("References", d.Refs)
	}
	h("MIME-Version", "1.0")
	h(KeenHeader, "1")

	if len(d.Attachments) == 0 {
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		if err := writeText(&buf, d.Body); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	mw := multipart.NewWriter(&buf)
	h("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	buf.WriteString("\r\n")
	tp, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
	if err != nil {
		return nil, err
	}
	if err := writeText(tp, d.Body); err != nil {
		return nil, err
	}
	for _, f := range d.Attachments {
		name := filepath.Base(f.Name)
		ct := mime.TypeByExtension(filepath.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		ap, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(ct, map[string]string{"name": name})},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": name})},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", name, err)
		}
		enc := base64.StdEncoding.EncodeToString(f.Data)
		for len(enc) > 76 {
			io.WriteString(ap, enc[:76]+"\r\n")
			enc = enc[76:]
		}
		io.WriteString(ap, enc+"\r\n")
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeText(w io.Writer, body string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"))); err != nil {
		return err
	}
	return qp.Close()
}

// addressHeader validates each address and joins them as a folded header value.
func addressHeader(list []string) (string, error) {
	out := make([]string, 0, len(list))
	for _, raw := range list {
		a, err := mail.ParseAddress(raw)
		if err != nil {
			return "", fmt.Errorf("invalid address %q", raw)
		}
		out = append(out, a.String())
	}
	return strings.Join(out, ",\r\n "), nil
}
