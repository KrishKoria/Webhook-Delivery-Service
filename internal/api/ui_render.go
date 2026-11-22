package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin/render"
)

// partialPrefix marks a render name as a fragment rather than a full page.
const partialPrefix = "partial:"

// uiRenderer is a Gin HTMLRender that keeps one template set per page.
// Each set is layouts/base.html + partials/*.html + pages/<name>.html, so every
// page can define its own "title" and "content" blocks without name clashes.
type uiRenderer struct {
	pages    map[string]*template.Template
	partials *template.Template
}

// NewUIRenderer parses the admin UI templates from fsys.
// Render a page with c.HTML(code, "<page name>", data) and a fragment with
// c.HTML(code, "partial:<template name>", data).
func NewUIRenderer(fsys fs.FS) (render.HTMLRender, error) {
	partials, err := template.New("partials").Funcs(uiFuncs).ParseFS(fsys, "partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse partials: %w", err)
	}
	pageFiles, err := fs.Glob(fsys, "pages/*.html")
	if err != nil {
		return nil, err
	}
	r := &uiRenderer{pages: make(map[string]*template.Template, len(pageFiles)), partials: partials}
	for _, file := range pageFiles {
		name := strings.TrimSuffix(path.Base(file), ".html")
		t, err := template.New(name).Funcs(uiFuncs).ParseFS(fsys, "layouts/base.html", "partials/*.html", file)
		if err != nil {
			return nil, fmt.Errorf("parse page %s: %w", name, err)
		}
		r.pages[name] = t
	}
	return r, nil
}

func (r *uiRenderer) Instance(name string, data any) render.Render {
	if p, ok := strings.CutPrefix(name, partialPrefix); ok {
		return render.HTML{Template: r.partials, Name: p, Data: data}
	}
	t, ok := r.pages[name]
	if !ok {
		// Surface the bug as a template execution error instead of a nil dereference.
		t = template.Must(template.New("missing").Parse(`{{define "base"}}{{.missing}}{{end}}`))
		return render.HTML{Template: t, Name: "unknown page " + name, Data: data}
	}
	return render.HTML{Template: t, Name: "base", Data: data}
}

var uiFuncs = template.FuncMap{
	"add":        func(a, b int) int { return a + b },
	"subtract":   func(a, b int) int { return a - b },
	"timeTag":    timeTag,
	"badge":      badge,
	"prettyJSON": prettyJSON,
	"pct":        pct,
	"widthClass": widthClass,
	"rateClass":  rateClass,
	"nullStr":    func(s sql.NullString) string { return s.String },
	"short":      shortID,
	"httpClass":  httpClass,
	"plural": func(n int64, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	},
	"dict": func(kv ...any) (map[string]any, error) {
		if len(kv)%2 != 0 {
			return nil, fmt.Errorf("dict needs key/value pairs")
		}
		m := make(map[string]any, len(kv)/2)
		for i := 0; i < len(kv); i += 2 {
			k, ok := kv[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict key %v is not a string", kv[i])
			}
			m[k] = kv[i+1]
		}
		return m, nil
	},
	"canRedeliver": func(status sql.NullString) bool {
		return status.String == "delivered" || status.String == "failed"
	},
}

// timeTag renders a <time> element with an RFC 3339 UTC datetime and an absolute
// UTC label. app.js upgrades the label to local and relative time.
func timeTag(v any) template.HTML {
	t, ok := asTime(v)
	if !ok {
		return `<span class="muted">never</span>`
	}
	t = t.UTC()
	return template.HTML(fmt.Sprintf(`<time datetime="%s">%s</time>`,
		t.Format(time.RFC3339), t.Format("2006-01-02 15:04:05 UTC")))
}

func asTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, !x.IsZero()
	case *time.Time:
		if x == nil {
			return time.Time{}, false
		}
		return *x, !x.IsZero()
	case sql.NullTime:
		return x.Time, x.Valid && !x.Time.IsZero()
	case string:
		return parseDBTimeUTC(x)
	case []byte:
		return parseDBTimeUTC(string(x))
	}
	return time.Time{}, false
}

// parseDBTime parses the timestamp encodings SQLite drivers hand back for
// aggregates, where the declared column type is lost.
func parseDBTimeUTC(s string) (time.Time, bool) {
	for _, layout := range []string{time.DateTime, time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

type badgeSpec struct{ tone, label string }

// badgeTones maps (kind, status) to a tone class and a human label.
var badgeTones = map[string]map[string]badgeSpec{
	"task": {
		"pending":   {"info", "pending"},
		"in_flight": {"progress", "in flight"},
		"delivered": {"ok", "delivered"},
		"failed":    {"bad", "failed"},
	},
	"outcome": {
		"success":        {"ok", "success"},
		"failed_attempt": {"bad", "failed attempt"},
	},
	"dlq": {
		"dead":    {"bad", "dead"},
		"retried": {"neutral", "retried"},
	},
	"scheduled": {
		"pending":   {"info", "pending"},
		"enqueued":  {"ok", "enqueued"},
		"delivered": {"ok", "enqueued"}, // legacy rows written before the rename
		"cancelled": {"neutral", "cancelled"},
		"failed":    {"bad", "failed"},
	},
	"subscription": {
		"active": {"ok", "active"},
		"paused": {"warn", "paused"},
	},
}

// badge renders a status pill; unknown statuses fall back to a neutral tone
// with the raw value so new states are visible rather than mislabelled.
func badge(kind string, status any) template.HTML {
	var s string
	switch x := status.(type) {
	case string:
		s = x
	case sql.NullString:
		s = x.String
	default:
		s = fmt.Sprint(x)
	}
	if s == "" {
		return `<span class="muted">none</span>`
	}
	spec, ok := badgeTones[kind][s]
	if !ok {
		spec = badgeSpec{"neutral", strings.ReplaceAll(s, "_", " ")}
	}
	return template.HTML(fmt.Sprintf(`<span class="badge badge--%s">%s</span>`,
		spec.tone, template.HTMLEscapeString(spec.label)))
}

// prettyJSON indents valid JSON and returns anything else unchanged.
func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// pct returns part/total as a whole percentage in [0, 100].
func pct(part, total int64) int64 {
	if total <= 0 || part <= 0 {
		return 0
	}
	if part >= total {
		return 100
	}
	return part * 100 / total
}

// widthClass maps a percentage to one of the w-0..w-100 utility classes (5% steps),
// since the CSP forbids inline style attributes. Non-zero values never round to 0.
func widthClass(p int64) string {
	if p <= 0 {
		return "w-0"
	}
	if p >= 100 {
		return "w-100"
	}
	step := (p + 2) / 5 * 5
	if step == 0 {
		step = 5
	}
	return fmt.Sprintf("w-%d", step)
}

// rateClass colours a success rate by threshold.
func rateClass(rate float64, total int64) string {
	switch {
	case total == 0:
		return "fill--neutral"
	case rate >= 99:
		return "fill--ok"
	case rate >= 90:
		return "fill--warn"
	default:
		return "fill--bad"
	}
}

func httpClass(code sql.NullInt64) string {
	switch {
	case !code.Valid:
		return "muted"
	case code.Int64 >= 200 && code.Int64 < 300:
		return "http-ok"
	case code.Int64 >= 500:
		return "http-bad"
	default:
		return "http-warn"
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
