package paseobridgert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

var nativeCursor = regexp.MustCompile(`^[a-f0-9-]{36}:\d{1,12}$`)

type nativeStreamReader struct {
	*io.PipeReader
	cancel context.CancelFunc
}

func (r *nativeStreamReader) Close() error { r.cancel(); return r.PipeReader.Close() }
func (c *nativeChannel) stream(r *http.Request, cancel context.CancelFunc, host, agent string, initial map[string]any, activity bool) *http.Response {
	reader, writer := io.Pipe()
	body := &nativeStreamReader{reader, cancel}
	go func() {
		defer cancel()
		defer c.close()
		defer writer.Close()
		stop := context.AfterFunc(c.ctx, func() { _ = writer.CloseWithError(c.ctx.Err()) })
		defer stop()
		epoch := nativeUUID()
		sequence := 0
		totalBytes := 0
		emit := func(kind string, event map[string]any) error {
			event["kind"] = kind
			raw, err := json.Marshal(event)
			if err != nil || len(raw) > 1<<20 {
				return nativeFailure
			}
			sequence++
			wire := []byte(fmt.Sprintf("id: %s:%d\nevent: %s\ndata: %s\n\n", epoch, sequence, kind, raw))
			totalBytes += len(wire)
			if totalBytes > maxEncryptedResponse {
				return nativeFailure
			}
			_, err = writer.Write(wire)
			return err
		}
		fail := func() {
			_ = emit("error", map[string]any{"error": map[string]string{"code": "NATIVE_STREAM", "message": "Encrypted native observation failed; reconnect for a fresh snapshot"}})
		}
		if r.Header.Get("Last-Event-ID") != "" {
			if emit("resync", map[string]any{"reason": "No durable replay; fresh snapshot follows"}) != nil {
				return
			}
		}
		if emit("snapshot", initial) != nil || emit("connection", map[string]any{"state": "connected"}) != nil {
			return
		}
		previousSnapshot, _ := json.Marshal(initial["agent"])
		var previousTimeline []byte
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			if activity {
				data, err := c.activity(host, agent, 20)
				if err != nil {
					fail()
					return
				}
				timeline := objectField(data, "timeline")
				comparable := map[string]any{}
				for k, v := range timeline {
					if k != "requestId" && k != "agent" {
						comparable[k] = v
					}
				}
				raw, _ := json.Marshal(comparable)
				if !bytes.Equal(raw, previousTimeline) {
					if emit("activity", data) != nil {
						return
					}
					previousTimeline = raw
				}
			}
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
			}
			snapshot, err := c.snapshot(host, agent)
			if err != nil {
				fail()
				return
			}
			raw, _ := json.Marshal(snapshot["agent"])
			if !bytes.Equal(raw, previousSnapshot) {
				if emit("snapshot", snapshot) != nil {
					return
				}
				previousSnapshot = raw
			} else {
				if _, err = writer.Write([]byte(": heartbeat\n\n")); err != nil {
					return
				}
			}
		}
	}()
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": []string{"text/event-stream"}, "Cache-Control": []string{"no-store"}}, Body: body, ContentLength: -1, Request: r}
}
