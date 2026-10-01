package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

func sourceCopy(ctx context.Context, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 64<<10)
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		n, e := src.Read(buf)
		if n > 0 {
			if e := ctx.Err(); e != nil {
				return e
			}
			written, we := dst.Write(buf[:n])
			if we != nil {
				return we
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}
func (s *Server) handleSourceContent(w http.ResponseWriter, r *http.Request) {
	if _, ok := sourceReadRequest(w, r); !ok || !s.sourceService(w, r) {
		return
	}
	run, ok := s.sourceLoad(w, r, false)
	if !ok {
		return
	}
	actor, _ := sourceActor(r)
	stream, e := s.Source.OpenOutput(r.Context(), actor, r.PathValue("id"), run.ID)
	if e != nil {
		s.sourceResult(w, r, run, e)
		return
	}
	defer stream.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.FormatInt(run.Output.SizeBytes, 10))
	w.Header().Set("Content-Disposition", `inline; filename="source-image.png"`)
	// OpenOutput only returns an already verified local stream. Network grants,
	// signatures and partially verified provider bytes never enter this response.
	_ = sourceCopy(r.Context(), w, stream)
}
func (s *Server) handleSourceExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := sourceReadRequest(w, r); !ok || !s.sourceService(w, r) {
		return
	}
	run, ok := s.sourceLoad(w, r, false)
	if !ok {
		return
	}
	if run.Deleted || run.Output == nil {
		s.sourceResult(w, r, run, si.ErrConflict)
		return
	}
	actor, _ := sourceActor(r)
	stream, e := s.Source.OpenOutput(r.Context(), actor, r.PathValue("id"), run.ID)
	if e != nil {
		s.sourceResult(w, r, run, e)
		return
	}
	defer stream.Close()
	quality, e := sourceFrozenQuality(run)
	if e != nil {
		s.sourceResult(w, r, run, e)
		return
	}
	snapshot, _ := json.Marshal(map[string]any{"report_version": "product-source-export/v1", "run": sourceView(run), "this_download_content_verification": "verified", "this_download_billing_check": "charged", "frozen_report_content_verification": "NOT_RUN", "visual_quality": "unknown", "human_adoption": "NOT_RUN"})
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="source-image.zip"`)
	archive := zip.NewWriter(w)
	image, e := archive.CreateHeader(&zip.FileHeader{Name: "image.png", Method: zip.Store})
	if e != nil {
		return
	}
	if sourceCopy(r.Context(), image, stream) != nil {
		return
	}
	for _, entry := range []struct {
		name string
		raw  []byte
	}{{"quality.json", quality}, {"snapshot.json", snapshot}} {
		if r.Context().Err() != nil {
			return
		}
		out, e := archive.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if e != nil {
			return
		}
		if _, e = out.Write(entry.raw); e != nil {
			return
		}
	}
	_ = archive.Close()
}
