package static

import (
 "embed"
 "io/fs"
 "net/http"
 "strings"
)

//go:embed all:dist
var embedded embed.FS

func Handler() http.Handler {
 sub, err := fs.Sub(embedded, "dist")
 if err != nil { panic(err) }
 files := http.FileServer(http.FS(sub))
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  if r.URL.Path == "/" { r.URL.Path="/index.html"; files.ServeHTTP(w,r); return }
  name:=strings.TrimPrefix(r.URL.Path,"/")
  if name=="" { name="index.html" }
  if _,err:=fs.Stat(sub,name);err!=nil { r.URL.Path="/index.html" }
  files.ServeHTTP(w,r)
 })
}
