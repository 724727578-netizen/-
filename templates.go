package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
)

//go:embed templates/*.html
var templatesFS embed.FS

// pageTemplates 在 init() 中一次性解析并缓存，避免每次请求重复解析。
var pageTemplates = make(map[string]*template.Template)

func init() {
	funcMap := template.FuncMap{
		"inc": func(i int) int { return i + 1 },
		"status": func(item FileItem) string {
			if item.Locked {
				return "已锁定"
			}
			return "待处理"
		},
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
	}

	baseBytes, err := templatesFS.ReadFile("templates/base.html")
	if err != nil {
		log.Fatalf("[GT_SPLIT_GO] 读取 base 模板失败：%v", err)
	}
	base := string(baseBytes)

	pages := map[string]string{
		"index": "templates/index.html",
		"mode1": "templates/mode1.html",
		"mode2": "templates/mode2.html",
		"mode3": "templates/mode3.html",
		"mode4": "templates/mode4.html",
		"mode5": "templates/mode5.html",
		"mode6": "templates/mode6.html",
	}
	for name, file := range pages {
		contentBytes, err := templatesFS.ReadFile(file)
		if err != nil {
			log.Fatalf("[GT_SPLIT_GO] 读取 %s 模板失败：%v", name, err)
		}
		pageTemplates[name] = template.Must(
			template.New("").Funcs(funcMap).Parse(base + string(contentBytes)),
		)
	}
}
