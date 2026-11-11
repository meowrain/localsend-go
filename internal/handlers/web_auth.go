package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net/http"
	"strings"

	"github.com/meowrain/localsend-go/internal/config"
)

// Web 模式的访问控制。
//
// Web 模式默认「信任局域网」，与 LocalSend 协议一致；但一旦用户配置了
// functions.pin（或 --pin），Web 页面也必须受保护，否则 PIN 只是挡住了
// LocalSend 客户端，浏览器仍然可以任意读写 uploads/。
//
// 认证方式刻意做得很轻：
//   - 用户在登录页输入 PIN，校验通过后下发 HttpOnly Cookie；
//   - Cookie 值是 HMAC(进程级随机密钥, PIN)，不落盘、不含 PIN 明文；
//   - 也支持协议风格的 ?pin=，便于脚本与二维码链接；
//   - 失败计数复用 pinGuard（按 IP、3 次/10 分钟），防止在线爆破。
//
// 这仍然不是「公网可用」级别的认证：请勿把 Web 模式直接暴露到公网。

const (
	webPINQuery  = "pin"
	webPINCookie = "localsend_web_pin"
)

// webAuthKey 是进程级随机密钥，重启即失效（用户需要重新登录）。
var webAuthKey = func() []byte {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// 极端情况下退化为全零：Cookie 只在本次进程内有效，影响有限
		return make([]byte, 32)
	}
	return buf
}()

// webPINEnabled 返回 Web 模式是否需要 PIN。
func webPINEnabled() bool {
	return config.ConfigData.ReceiverPIN() != ""
}

// webPINToken 计算 PIN 对应的 Cookie 值。
func webPINToken(pin string) string {
	mac := hmac.New(sha256.New, webAuthKey)
	mac.Write([]byte(pin))
	return hex.EncodeToString(mac.Sum(nil))
}

// webAuthorized 判断请求是否已通过 Web 认证。
//
// want 为空（未配置 PIN）时一律放行；Cookie 校验用常量时间比较。
// 注意：这里不再读取 ?pin= —— 查询参数只用于「首次登录换取 Cookie」，
// 否则一个 URL 被复制/记录后就会永久携带 PIN。
func webAuthorized(r *http.Request) bool {
	want := config.ConfigData.ReceiverPIN()
	if want == "" {
		return true
	}
	c, err := r.Cookie(webPINCookie)
	if err != nil {
		return false
	}
	expected := webPINToken(want)
	got := c.Value
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// tryPINLogin 校验请求携带的 PIN（查询参数或表单），成功时写 Cookie。
// 返回值只表示「认证成功」，失败原因（401/429）由调用方根据 status 决定响应。
func tryPINLogin(w http.ResponseWriter, r *http.Request, given string) (ok bool, status int, msg string) {
	want := config.ConfigData.ReceiverPIN()
	ok, status, msg = checkPINWithLimit(want, clientIP(r), given)
	if !ok {
		return false, status, msg
	}
	http.SetCookie(w, &http.Cookie{
		Name:     webPINCookie,
		Value:    webPINToken(want),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// 不设 Secure：Web 模式按协议服务在明文 HTTP 上，设了会导致 Cookie 无法回传
	})
	return true, 0, ""
}

// WebAuth 是 Web 页面的认证中间件。
//
// 逻辑：
//   - 未配置 PIN：直接放行；
//   - 已认证（Cookie）：放行；
//   - 带 ?pin=：校验成功后设 Cookie 并重定向到去掉 pin 的同一地址；
//   - 其他 GET/HEAD：渲染登录页；
//   - 其他方法或 /api/*：401/429 JSON。
func WebAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if webAuthorized(r) {
			next(w, r)
			return
		}

		if given := r.URL.Query().Get(webPINQuery); strings.TrimSpace(given) != "" {
			if ok, status, msg := tryPINLogin(w, r, given); !ok {
				respondWebAuthFailure(w, r, status, msg)
				return
			}
			redirectWithoutPIN(w, r)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeWebError(w, http.StatusUnauthorized, "需要 PIN")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeWebError(w, http.StatusUnauthorized, "需要 PIN")
			return
		}
		renderLoginPage(w, r, "")
	}
}

// respondWebAuthFailure 根据 PIN 校验失败原因返回响应。
func respondWebAuthFailure(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if strings.HasPrefix(r.URL.Path, "/api/") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		writeWebError(w, status, msg)
		return
	}
	renderLoginPage(w, r, msg)
}

// redirectWithoutPIN 生成去掉 ?pin= 的重定向地址，避免 PIN 留在浏览器地址栏/历史里。
func redirectWithoutPIN(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Del(webPINQuery)
	target := r.URL.Path
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// loginTemplate 是登录页模板；保持内联，避免为一张表单增加内嵌文件。
var loginTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>LocalSendGo · 需要 PIN</title>
<style>
  :root { color-scheme: light dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
         font-family: system-ui,-apple-system,"Segoe UI",Roboto,"Noto Sans SC",sans-serif;
         background:linear-gradient(135deg,#eef2ff,#f8fafc); color:#0f172a; }
  .card { width:min(92vw,360px); background:#fff; border-radius:16px; padding:28px 24px;
          box-shadow:0 20px 50px rgba(15,23,42,.12); }
  h1 { margin:0 0 6px; font-size:1.15rem; }
  p { margin:0 0 18px; color:#64748b; font-size:.9rem; }
  input { width:100%; box-sizing:border-box; padding:12px 14px; font-size:1rem; border-radius:10px;
          border:1px solid #cbd5e1; outline:none; }
  input:focus { border-color:#4f46e5; box-shadow:0 0 0 3px rgba(79,70,229,.15); }
  button { margin-top:14px; width:100%; padding:12px; border:0; border-radius:10px; cursor:pointer;
           background:#4f46e5; color:#fff; font-size:1rem; font-weight:600; }
  button:hover { background:#4338ca; }
  .err { margin-top:12px; color:#dc2626; font-size:.85rem; }
  @media (prefers-color-scheme: dark) {
    body { background:linear-gradient(135deg,#0b1120,#111827); color:#e2e8f0; }
    .card { background:#111827; box-shadow:0 20px 50px rgba(0,0,0,.5); }
    input { background:#0b1120; border-color:#334155; color:#e2e8f0; }
  }
</style>
</head>
<body>
  <form class="card" method="get" action="">
    <h1>🔒 需要 PIN</h1>
    <p>这台设备启用了 Web 访问 PIN，请输入后继续。</p>
    <input name="pin" type="password" inputmode="numeric" autocomplete="current-password"
           placeholder="请输入 PIN" autofocus required>
    <button type="submit">进入</button>
    {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  </form>
</body>
</html>`))

// renderLoginPage 渲染登录页。
func renderLoginPage(w http.ResponseWriter, r *http.Request, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if errMsg != "" {
		errMsg = "PIN 错误或尝试次数过多，请稍后再试"
	}
	_ = loginTemplate.Execute(w, map[string]string{"Error": errMsg})
}
