// Package templates 保留给历史引用。
//
// Web 页面已经迁到 internal/handlers/web/ 并在 handlers 包里直接内嵌
// （go:embed 不能跨包），这里不再嵌入任何文件。
// 保留这个包是为了不让外部引用突然失效；新代码不要再用它。
package templates
