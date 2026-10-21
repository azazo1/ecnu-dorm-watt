package server

import (
	"bufio"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// csvBufferSize 是 CSV 输出的缓冲区大小.
const csvBufferSize = 32 * 1024

// csvFlushEvery 是流式输出时每多少行刷新一次.
const csvFlushEvery = 1000

// writeCSVHeader 设置 CSV 下载响应头.
func (s *Server) writeCSVHeader(w http.ResponseWriter, filename string) {
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", contentDisposition(filename))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// contentDisposition 构造同时兼容新旧浏览器的文件名声明.
func contentDisposition(filename string) string {
	fallback := strings.NewReplacer(`\`, "_", `"`, "_", "\r", "_", "\n", "_").Replace(filename)
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + rfc5987Encode(filename)
}

// rfc5987Encode 按 RFC 5987 对文件名做百分号编码.
func rfc5987Encode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case strings.IndexByte("!#$&+-.^_`|~", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0x0f])
		}
	}
	return b.String()
}

// streamSamplesCSV 把样本流写成 CSV.
//
// 此时响应头已经写出, 因此写失败只能记录日志.
func (s *Server) streamSamplesCSV(w http.ResponseWriter, r *http.Request, withHeader bool, stream func(func(store.Sample) error) error) {
	buffered := bufio.NewWriterSize(w, csvBufferSize)
	cw := csv.NewWriter(buffered)

	var writeErr error
	if withHeader {
		writeErr = cw.Write([]string{"time", "degree"})
	}
	rows := 0
	if writeErr == nil {
		writeErr = stream(func(sample store.Sample) error {
			record := []string{s.formatCSVTime(sample.Time), formatCSVDegree(sample.Degree)}
			if err := cw.Write(record); err != nil {
				return err
			}
			rows++
			if rows%csvFlushEvery == 0 {
				cw.Flush()
				if err := cw.Error(); err != nil {
					return err
				}
				if err := buffered.Flush(); err != nil {
					return err
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			return nil
		})
	}
	cw.Flush()
	if err := cw.Error(); err == nil && writeErr == nil {
		writeErr = buffered.Flush()
	} else if writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		s.logger.Warn("CSV 输出中断", "path", r.URL.Path, "request_id", requestIDFrom(r.Context()), "rows", rows, "error", writeErr)
	}
}

// formatCSVTime 按 RFC3339 输出样本时间.
func (s *Server) formatCSVTime(t time.Time) string {
	return s.exportTime(t).Format(time.RFC3339)
}

// formatCSVDegree 按两位小数输出电量.
func formatCSVDegree(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
