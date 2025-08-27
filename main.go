package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	htmlstd "html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

type Config struct {
	CNAME           string            `json:"cname"`
	GlobalOG        string            `json:"globalOG"`
	DefaultRedirect string            `json:"defaultRedirect"`
	Routes          map[string]string `json:"routes"`
}

type OG struct {
	Title       string
	Description string
	Image       string
}

func main() {
	var cfgPath, outDir string
	flag.StringVar(&cfgPath, "config", "routes.json", "path to routes.json")
	flag.StringVar(&outDir, "out", ".", "output directory")
	flag.Parse()

	cfg, err := loadConfig(cfgPath)
	must(err)

	if strings.TrimSpace(cfg.CNAME) != "" {
		must(os.WriteFile(filepath.Join(outDir, "CNAME"), []byte(cfg.CNAME+"\n"), 0644))
	}

	for p, to := range cfg.Routes {
		routePath := cleanRoutePath(p)
		log.Printf("fetching OG: %s -> %s", routePath, to)
		og, err := fetchOG(to)
		if err != nil {
			log.Printf("warn: OG fetch failed for %s: %v (using fallbacks)", to, err)
		}
		if og.Image == "" && cfg.GlobalOG != "" {
			og.Image = cfg.GlobalOG
		}
		if og.Title == "" {
			og.Title = "UniGoods"
		}
		if og.Description == "" {
			og.Description = "UniGoods link"
		}
		if og.Image != "" {
			if abs, err := absolutize(og.Image, to); err == nil {
				og.Image = abs
			}
		}

		destDir := filepath.Join(outDir, strings.TrimPrefix(routePath, "/"))
		must(os.MkdirAll(destDir, 0755))
		htmlPage := buildHTML(routePath, to, og)
		must(os.WriteFile(filepath.Join(destDir, "index.html"), []byte(htmlPage), 0644))
	}

	if strings.TrimSpace(cfg.DefaultRedirect) != "" {
		og := OG{
			Title:       "UniGoods",
			Description: "유니굿즈 숍으로 이동합니다.",
			Image:       cfg.GlobalOG,
		}
		page := buildHTML("/404", cfg.DefaultRedirect, og)
		must(os.WriteFile(filepath.Join(outDir, "404.html"), []byte(page), 0644))
	}

	log.Println("✅ done.")
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func cleanRoutePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}

func fetchOG(target string) (OG, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return OG{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7")

	res, err := client.Do(req)
	if err != nil {
		return OG{}, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return OG{}, err
	}
	return parseOGHTML(body, target), nil
}

func parseOGHTML(body []byte, base string) OG {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return OG{}
	}
	var og OG
	var f func(*xhtml.Node)
	f = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && strings.EqualFold(n.Data, "meta") {
			var prop, name, cont string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "property":
					prop = strings.ToLower(strings.TrimSpace(a.Val))
				case "name":
					name = strings.ToLower(strings.TrimSpace(a.Val))
				case "content":
					cont = strings.TrimSpace(a.Val)
				}
			}
			key := prop
			if key == "" {
				key = name
			}
			switch key {
			case "og:title":
				og.Title = cont
			case "og:description":
				og.Description = cont
			case "og:image":
				og.Image = cont
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return og
}

func absolutize(raw string, baseStr string) (string, error) {
	if raw == "" {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw, err
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	base, err := url.Parse(baseStr)
	if err != nil {
		return raw, err
	}
	if u.Host != "" && u.Scheme == "" {
		u.Scheme = base.Scheme
		return u.String(), nil
	}
	return base.ResolveReference(u).String(), nil
}

func buildHTML(path, to string, og OG) string {
	title := htmlstd.EscapeString(og.Title)
	desc := htmlstd.EscapeString(og.Description)
	img := htmlstd.EscapeString(og.Image)
	shopURL := "https://shop.unigoods.im" + path
	shopURL = htmlstd.EscapeString(shopURL)
	toEsc := htmlstd.EscapeString(to)

	tpl := `<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<title>%s</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="%s">
<meta name="robots" content="noindex">
<meta property="og:type" content="website">
<meta property="og:title" content="%s">
<meta property="og:description" content="%s">
<meta property="og:image" content="%s">
<meta property="og:url" content="%s">
<meta name="twitter:card" content="summary_large_image">
<link rel="canonical" href="%s">
<script>(function(){ window.location.replace("%s"); })();</script>
<style>html,body{background:#fff;margin:0;height:100%%;display:flex;align-items:center;justify-content:center;font:16px/1.4 -apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica,Arial,Apple SD Gothic Neo,Noto Sans KR,sans-serif;color:#111}</style>
</head>
<body>
<noscript>자바스크립트가 꺼져 있어요. <a href="%s">여기를 눌러 이동</a>하세요.</noscript>
</body>
</html>`
	return fmt.Sprintf(tpl, title, desc, title, desc, img, shopURL, toEsc, toEsc, toEsc)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
