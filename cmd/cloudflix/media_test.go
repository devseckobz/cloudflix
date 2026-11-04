package main

import (
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestMediaFileWhitelist(t *testing.T){
 cases:=[]struct{path string;want bool}{
  {"Movie.2026.mp4",true},{"Movie.2026.MP4",true},
  {"Movie.2026.srt",true},{"Movie.2026.en.SRT",true},
  {"Movie.mkv",true},{"Movie.avi",false},{"Movie.webm",false},
  {"Movie.vtt",false},{"Movie.exe",false},{"Movie.mp4.exe",false},
  {"../movie.mp4",false},{"sub/../movie.mp4",false},
  {"sub/Movie.mp4",true},
 }
 for _,tc:=range cases{
  if got:=allowedMediaPath(tc.path);got!=tc.want{t.Errorf("allowedMediaPath(%q): %v, want %v",tc.path,got,tc.want)}
 }
}

func TestSelectMediaFilesRequiresMP4OrMKV(t *testing.T){
 files,video:=selectedMediaFiles([]string{"poster.jpg","subtitles.srt","unwanted.exe"})
 if video || len(files)!=1 || files[0]!="subtitles.srt"{t.Fatalf("SRT-only torrent must be rejected: %v video=%v",files,video)}
 files,video=selectedMediaFiles([]string{"movie.mkv","movie.srt","unwanted.exe"})
 if !video || len(files)!=2{t.Fatalf("MKV and SRT must be selected: %v video=%v",files,video)}
 files,video=selectedMediaFiles([]string{"movie.mp4","movie.en.srt","sample.mkv","installer.sh","readme.txt"})
 if !video || len(files)!=3{t.Fatalf("unexpected file selections: %v",files)}
}

func TestServeMediaSupportsRangeRequests(t *testing.T){
 dir:=t.TempDir()
 data:=strings.Repeat("abcdefghijklmnopqrstuvwxyz",200)
 if err:=os.WriteFile(filepath.Join(dir,"Movie.mp4"),[]byte(data),0600);err!=nil{t.Fatal(err)}
 root,err:=os.OpenRoot(dir);if err!=nil{t.Fatal(err)}
 defer root.Close()
 req:=httptest.NewRequest(http.MethodGet,"/api/media/test?path=Movie.mp4",nil)
 req.Header.Set("Range","bytes=10-19")
 recorder:=httptest.NewRecorder()
 serveMediaFile(recorder,req,root,"Movie.mp4")
 if recorder.Code!=http.StatusPartialContent{t.Fatalf("expected 206, got %d: %s",recorder.Code,recorder.Body.String())}
 if recorder.Body.String()!=data[10:20]{t.Fatalf("incorrect byte range returned")}
 if recorder.Header().Get("Content-Type")!="video/mp4"{t.Fatalf("incorrect video MIME type")}
 if recorder.Header().Get("Content-Range")==""{t.Fatal("missing content range")}
}

func TestSRTRenderedAsWebVTT(t *testing.T){
 dir:=t.TempDir()
 raw:="1\r\n00:00:01,200 --> 00:00:03,650\r\nHello, Cloudflix!\r\n"
 if err:=os.WriteFile(filepath.Join(dir,"movie.en.srt"),[]byte(raw),0600);err!=nil{t.Fatal(err)}
 root,err:=os.OpenRoot(dir);if err!=nil{t.Fatal(err)}
 defer root.Close()
 recorder:=httptest.NewRecorder()
 serveMediaFile(recorder,httptest.NewRequest("GET","/sub",nil),root,"movie.en.srt")
 if recorder.Code!=http.StatusOK{t.Fatalf("unexpected status %d",recorder.Code)}
 if recorder.Header().Get("Content-Type")!="text/vtt; charset=utf-8"{t.Fatalf("invalid subtitle MIME %q",recorder.Header().Get("Content-Type"))}
 body:=recorder.Body.String()
 if !strings.HasPrefix(body,"WEBVTT\n\n") || !strings.Contains(body,"00:00:01.200 --> 00:00:03.650"){t.Fatalf("malformed WebVTT subtitle: %q",body)}
}

func TestMediaRejectsOtherTorrentFilesAndTraversal(t *testing.T){
 dir:=t.TempDir()
 for name,text:=range map[string]string{"unwanted.exe":"MZ","movie.mkv":"mkv","movie.mp4":"mp4"}{
  if err:=os.WriteFile(filepath.Join(dir,name),[]byte(text),0600);err!=nil{t.Fatal(err)}
 }
 root,err:=os.OpenRoot(dir);if err!=nil{t.Fatal(err)}
 defer root.Close()
 for _,path:=range []string{"unwanted.exe","movie.mkv","../movie.mp4"}{
  recorder:=httptest.NewRecorder()
  serveMediaFile(recorder,httptest.NewRequest("GET","/",nil),root,path)
  if recorder.Code<400{t.Errorf("%q must be rejected, received %d",path,recorder.Code)}
 }
}
