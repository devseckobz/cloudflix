package static

import (
 "bytes"
 "embed"
 "io/fs"
 "net/http"
 "strings"
 "time"
)

//go:embed all:dist
var embedded embed.FS

func Handler() http.Handler {
 sub, err := fs.Sub(embedded, "dist")
 if err != nil { panic(err) }
 files := http.FileServer(http.FS(sub))
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  name:=strings.TrimPrefix(r.URL.Path,"/")
  if name=="" { name="index.html" }
  if _,err:=fs.Stat(sub,name);err!=nil { name="index.html" }
  if name=="index.html"{
   contents,err:=fs.ReadFile(sub,name)
   if err!=nil {http.Error(w,"frontend build unavailable",503);return}
   w.Header().Set("Content-Type","text/html; charset=utf-8")
   http.ServeContent(w,r,"cloudflix.html",time.Time{},bytes.NewReader(contents))
   return
  }
  files.ServeHTTP(w,r)
 })
}
