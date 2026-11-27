package main

// Download is the counterpart to streaming: /api/files must return the exact
// bytes with Content-Disposition: attachment, and must refuse every path the
// streaming endpoint would refuse.

import (
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "testing"
)

func downloadFixture(t *testing.T,content string)(*application,string){
 t.Helper()
 e:=perfEngine(t,1,1)
 rel:="Movie00/Movie00.mp4"
 local:=filepath.Join(e.downloads,filepath.FromSlash(rel))
 if err:=os.MkdirAll(filepath.Dir(local),0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(local,[]byte(content),0o600);err!=nil{t.Fatal(err)}
 return &application{engine:e},orderedEntries(e.entries)[0].hash
}

// downloadRequest builds the request the mux would produce, including the
// path wildcard the handler reads through r.PathValue.
func downloadRequest(t *testing.T,hash,path string)*http.Request{
 t.Helper()
 r:=sessionRequest(t,http.MethodGet,"/api/files/"+hash+"?path="+path,"","")
 r.SetPathValue("hash",hash)
 return r
}

func TestDownloadFileServesOriginalBytesAsAttachment(t *testing.T){
 const content="0123456789abcdef-original-media-bytes"
 app,hash:=downloadFixture(t,content)
 recorder:=httptest.NewRecorder()
 app.downloadFile(recorder,downloadRequest(t,hash,"Movie00/Movie00.mp4"))
 if recorder.Code!=http.StatusOK{t.Fatalf("download failed: %d %s",recorder.Code,recorder.Body.String())}
 if got:=recorder.Body.String();got!=content{t.Fatalf("bytes differ: %q",got)}
 if disposition:=recorder.Header().Get("Content-Disposition");disposition!="attachment"{
  t.Fatalf("expected attachment disposition, got %q",disposition)
 }
 if recorder.Header().Get("Content-Length")==""{
  t.Fatal("download must declare its length so the browser can show progress")
 }
}

func TestDownloadFileRejectsTraversalAndUnknownPaths(t *testing.T){
 app,hash:=downloadFixture(t,"x")
 for _,path:=range []string{"../escape.mp4","Movie00/../../etc/passwd","Movie00/Missing.mp4","","Movie00/script.sh"}{
  recorder:=httptest.NewRecorder()
  app.downloadFile(recorder,downloadRequest(t,hash,path))
  if recorder.Code==http.StatusOK{t.Fatalf("path %q was served",path)}
 }
}
