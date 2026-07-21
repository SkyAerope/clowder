package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SkyAerope/clowder/internal/rules"
)

// deliver 把已 dial 成功的 attempt 交付客户端（写 w）。
func (s *Server) deliver(w http.ResponseWriter, la *liveAttempt, reqModel string) *attemptOutcome {
	o := la.o
	defer func() { o.latencyMs = time.Since(la.start).Milliseconds() }()

	if la.hasWhole {
		body := la.whole
		if la.clientStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			o.firstByteSent = true
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(rewriteModelField(body, reqModel))
		}
		o.delivered = true
		return o
	}

	defer la.resp.Body.Close()
	reader := la.reader
	cand := la.cand
	clientGone := func() bool { return la.clientCtx.Err() != nil }

	var tee *bytes.Buffer
	teeLimit := s.App.Cfg.Capture.ResponseBufferKB << 10
	if s.App.Engine.NeedsSuccessBodyTee() || s.App.Engine.NeedsResponseCaptureTee() {
		tee = &bytes.Buffer{}
	}
	teeWrite := func(b []byte) {
		if tee != nil && tee.Len() < teeLimit {
			n := teeLimit - tee.Len()
			if n > len(b) {
				n = len(b)
			}
			tee.Write(b[:n])
		}
	}

	if la.aggregate {
		agg := &aggregator{}
		chunk := la.first
		for {
			if chunk.done {
				break
			}
			if chunk.data != nil {
				agg.feed(chunk.data)
				teeWrite(chunk.data)
			}
			var err error
			chunk, err = reader.next()
			if err != nil {
				if err == io.EOF {
					break
				}
				if clientGone() {
					o.canceled = true
					return o
				}
				o.event.Transport = rules.TransportStreamCut
				o.event.IsError = true
				o.transportErr = err
				if tee != nil {
					o.respBody = append(o.respBody[:0], tee.Bytes()...)
					o.event.Body = tee.String()
				}
				return o
			}
		}
		result := agg.result(reqModel)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(result)
		o.delivered = true
		o.firstByteSent = true
		if tee != nil {
			o.respBody = append(o.respBody[:0], tee.Bytes()...)
			o.event.Body = tee.String()
		}
		return o
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	rewriteModel := newModelRewriter(cand.Native, reqModel)
	writeChunk := func(c *sseChunk) bool {
		rewriteModel(c)
		if _, err := w.Write(c.raw); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	o.firstByteSent = true
	chunk := la.first
	for {
		if chunk.data != nil {
			teeWrite(chunk.data)
		}
		if !writeChunk(chunk) {
			o.canceled = true
			return o
		}
		if chunk.done {
			o.delivered = true
			break
		}
		var err error
		chunk, err = reader.next()
		if err != nil {
			if err == io.EOF {
				o.delivered = true
				break
			}
			if clientGone() {
				o.canceled = true
				return o
			}
			w.Write(sseErrorEvent(fmt.Sprintf("upstream stream cut (provider=%s): %v", cand.Provider, err)))
			if flusher != nil {
				flusher.Flush()
			}
			o.event.Transport = rules.TransportStreamCut
			o.event.IsError = true
			o.transportErr = err
			if tee != nil {
				o.respBody = append(o.respBody[:0], tee.Bytes()...)
				o.event.Body = tee.String()
			}
			return o
		}
	}
	if tee != nil {
		o.respBody = append(o.respBody[:0], tee.Bytes()...)
		o.event.Body = tee.String()
	}
	return o
}

// feedSuccessBody 同步设置成功事件 body（deliverWhole 已有全量 body 时）。
func (s *Server) feedSuccessBody(o *attemptOutcome, body string) {
	if s.App.Engine.NeedsSuccessBodyTee() {
		limit := s.App.Cfg.Capture.ResponseBufferKB << 10
		if len(body) > limit {
			body = body[:limit]
		}
		o.event.Body = body
	}
}
