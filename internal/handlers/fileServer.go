package handlers

import (
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/meowrain/localsend-go/internal/config"
)

// uploadDir 是 Web 页面展示与接收文件的根目录。
const uploadDir = "./uploads"

// webPageFiles 内嵌 Web 页面模板（go:embed 不能跨包，因此在 handlers 里再嵌一份）。
//
//go:embed web/*.html
var webPageFiles embed.FS

// webPageData 是渲染 Web 页面的模板数据。
type webPageData struct {
	// Path 是当前目录相对 uploads 的路径（无前导斜杠），根目录为 ""
	Path string
	// Parent 是上级目录，根目录时为 ""
	Parent string
	// Breadcrumbs 是路径分段，供前端/模板渲染面包屑
	Breadcrumbs []breadcrumb
	Files       []fileEntry
	// Token 是写操作的 CSRF token
	Token string
	// Device 是本机设备名（页面标题用）
	Device string
	// UploadLimitMB 展示给用户的上传大小上限，0 表示不限
	UploadLimitMB int64
	// PINEnabled 表示本机配置了 PIN（页面上提示）
	PINEnabled bool
}

type breadcrumb struct {
	Name string
	Path string
}

// fileEntry 是文件列表里的一项。
type fileEntry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	SizeStr string
	Icon    string
	Mime    string
	ModTime string
}

// templateFuncs 提供页面里用到的小工具。
var templateFuncs = template.FuncMap{
	"humanSize": humanSize,
}

// webPageTemplate 只解析一次：模板是内嵌的，运行期不会变。
var webPageTemplate = template.Must(
	template.New("cloudstorage.html").Funcs(templateFuncs).ParseFS(webPageFiles, "web/cloudstorage.html"),
)

// humanSize 把字节数格式化为人类可读的大小。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return formatInt(n) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return formatFloat(float64(n)/float64(div)) + " " + []string{"KB", "MB", "GB", "TB", "PB"}[exp]
}

func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

func formatFloat(f float64) string {
	// 保留一位小数，去掉无意义的 ".0"
	s := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 1, 64), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

// iconFor 按扩展名/MIME 选择 FontAwesome 图标。
func iconFor(name string, isDir bool) string {
	if isDir {
		return "fa-folder"
	}
	switch mimeTypeForPath(name) {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "image/svg+xml", "image/heic", "image/tiff":
		return "fa-file-image"
	case "video/mp4", "video/webm", "video/quicktime", "video/x-matroska":
		return "fa-file-video"
	case "audio/mpeg", "audio/ogg", "audio/wav", "audio/flac", "audio/aac":
		return "fa-file-audio"
	case "application/pdf":
		return "fa-file-pdf"
	case "application/zip", "application/gzip", "application/x-tar", "application/x-7z-compressed", "application/vnd.rar", "application/x-bzip2":
		return "fa-file-archive"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".csv", ".log", ".json", ".yaml", ".yml", ".toml", ".xml", ".html", ".css", ".js":
		return "fa-file-alt"
	case ".go", ".py", ".rs", ".c", ".cpp", ".h", ".java", ".ts", ".tsx", ".sh":
		return "fa-file-code"
	}
	return "fa-file"
}

// FileServerHandler 下载 uploads 下的文件：/uploads/<相对路径>。
func FileServerHandler(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/uploads/")
	if rel == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	abs, err := webFilePath(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if info.IsDir() {
		// 目录跳转到列表页，保持 URL 结构简单
		http.Redirect(w, r, "/"+url.PathEscape(strings.TrimSuffix(rel, "/"))+"/", http.StatusFound)
		return
	}
	serveUploadFile(w, r, abs, false)
}

// IndexFileHandler 渲染 Web 页面：/ 为根目录，/<相对目录>/ 为子目录。
//
// 只处理「目录列表」与单页应用入口；真正的文件下载走 /uploads/。
func IndexFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := strings.Trim(r.URL.Path, "/")
	// 保留旧版的 ?path= 用法（书签/外部链接不会失效）
	if q := r.URL.Query().Get("path"); q != "" {
		rel, _ = webRelPath(q)
	}
	clean, err := webRelPath(rel)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	abs, err := webFilePath(clean)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !info.IsDir() {
		// 直接访问 /foo.txt 时交给下载逻辑（保持地址栏原始 URL）
		serveUploadFile(w, r, abs, false)
		return
	}

	entries, err := listWebEntries(abs, clean)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := webPageData{
		Path:          clean,
		Parent:        parentOf(clean),
		Breadcrumbs:   breadcrumbsOf(clean),
		Files:         entries,
		Token:         WebToken(),
		Device:        config.ConfigData.NameOfDevice,
		UploadLimitMB: config.ConfigData.WebUploadLimitBytes() >> 20,
		PINEnabled:    config.ConfigData.ReceiverPIN() != "",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 页面里内联了 CSRF token，不能被任何中间层缓存
	w.Header().Set("Cache-Control", "no-store")
	// 页面里没有外部脚本；Google Fonts 已移除。CSP 允许多媒体预览与内联样式。
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; media-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; "+
			"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if err := webPageTemplate.ExecuteTemplate(w, "cloudstorage.html", data); err != nil {
		// 页面已经开始输出，只能记日志
		return
	}
}

// listWebEntries 列出目录内容（目录在前，按名称排序），跳过隐藏文件与逃逸的符号链接。
func listWebEntries(abs, rel string) ([]fileEntry, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]fileEntry, 0, len(entries))
	for _, de := range entries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(abs, name)
		st, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if st.Mode()&os.ModeSymlink != 0 {
			if err := checkWebPathSymlinks(full); err != nil {
				continue
			}
			resolved, err := os.Stat(full)
			if err != nil {
				continue
			}
			st = resolved
		}
		entryRel := path.Join(rel, name)
		item := fileEntry{
			Name:    name,
			Path:    entryRel,
			IsDir:   st.IsDir(),
			Size:    st.Size(),
			SizeStr: humanSize(st.Size()),
			Icon:    iconFor(name, st.IsDir()),
			ModTime: st.ModTime().Format("2006-01-02 15:04"),
		}
		if !st.IsDir() {
			item.Mime = mimeTypeForPath(name)
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// parentOf 返回上级目录相对路径，根目录返回 ""。
func parentOf(rel string) string {
	if rel == "" {
		return ""
	}
	parent := path.Dir(rel)
	if parent == "." || parent == "/" {
		return ""
	}
	return parent
}

// breadcrumbsOf 把相对路径拆成面包屑。
func breadcrumbsOf(rel string) []breadcrumb {
	if rel == "" {
		return nil
	}
	segs := strings.Split(rel, "/")
	out := make([]breadcrumb, 0, len(segs))
	acc := ""
	for _, seg := range segs {
		acc = path.Join(acc, seg)
		out = append(out, breadcrumb{Name: seg, Path: acc})
	}
	return out
}
