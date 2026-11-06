package main

import (
 "database/sql"
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

func TestPermanentMKVConversionPublishesBeforeRemovingOriginal(t *testing.T){
 e,_,hash:=prewarmFixture(t,true)
 base:=t.TempDir()
 e.downloads=filepath.Join(base,"downloads")
 e.libraryDir=filepath.Join(base,"library")
 e.cacheDir=filepath.Join(base,"playback-cache")
 if err:=os.Mkdir(e.downloads,0700);err!=nil{t.Fatal(err)}
 dbPath:=filepath.Join(base,"cloudflix.db")
 db,err:=sql.Open("sqlite",dbPath)
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 e.db=db
 _,err=db.Exec("PRAGMA journal_mode=WAL; CREATE TABLE torrents(hash TEXT PRIMARY KEY, magnet TEXT, paused INTEGER DEFAULT 0, title TEXT DEFAULT '', year TEXT DEFAULT '', poster TEXT DEFAULT '', backdrop TEXT DEFAULT '', looked_up INTEGER DEFAULT 0)")
 if err!=nil{t.Fatal(err)}
 if err:=ensureLibrarySchema(e.db);err!=nil{t.Fatal(err)}
 if _,err:=e.db.Exec("INSERT INTO torrents(hash,magnet,title) VALUES(?,'unusable magnet','Fixture')",hash);err!=nil{t.Fatal(err)}
 a:=&application{dataDir:base,engine:e,playback:newMKVManager()}
 ffmpeg,err:=mediatools.Resolve(base,"ffmpeg")
 if err!=nil || !strings.Contains(ffmpeg,"runtime-tools"){t.Skip("release payload not generated")}
 path:=e.torrentFilesLocked(e.entries[hash])[0]
 original:=filepath.Join(e.downloads,filepath.FromSlash(path))
 if err:=os.MkdirAll(filepath.Dir(original),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(original,[]byte("invalid MKV"),0600);err!=nil{t.Fatal(err)}
 convert:=func() *mkvJob{
  t.Helper()
  source,err:=os.Open(original)
  if err!=nil{t.Fatal(err)}
  key:=a.outputKey(hash,path,"")
  job:=&mkvJob{state:"queued"}
  a.playback.jobs[key]=job
  a.convertMKV(source,hash,path,key,job)
  return job
 }
 if job:=convert();job.state!="error" || !exists(original) || e.entries[hash].library!=nil{
  t.Fatalf("invalid MKV was not preserved: %+v",job)
 }
 subtitle:=filepath.Join(base,"fixture.srt")
 if err:=os.WriteFile(subtitle,[]byte("1\n00:00:00,100 --> 00:00:01,500\nFixture subtitle\n"),0600);err!=nil{t.Fatal(err)}
 args:=[]string{"-y","-nostdin","-v","error","-f","lavfi","-i","color=size=32x32:rate=5",
  "-i",subtitle,"-t","3","-c:v","libx264","-c:s","copy","-metadata:s:s:0","language=eng",original}
 if out,err:=exec.Command(ffmpeg,args...).CombinedOutput();err!=nil{t.Fatalf("fixture: %v: %s",err,out)}
 if job:=convert();job.state!="ready"{t.Fatalf("conversion: %+v",job)}
 if exists(original) || e.entries[hash].library==nil || e.entries[hash].torrent!=nil{t.Fatal("verified conversion did not replace the source")}
 if err:=db.Close();err!=nil{t.Fatal(err)}
 reopened,err:=sql.Open("sqlite",dbPath)
 if err!=nil{t.Fatal(err)}
 defer reopened.Close()
 restarted,err:=newEngine(e.downloads,reopened,"")
 if err!=nil{t.Fatal(err)}
 defer restarted.close()
 movie:=restarted.entries[hash]
 if movie==nil || movie.library==nil || movie.torrent!=nil || len(movie.library.Files)!=2{t.Fatalf("unexpected restarted movie: %+v",movie)}
 a.engine=restarted
 for _,file:=range movie.library.Files{
  r:=httptest.NewRequest(http.MethodGet,"/api/media/"+hash,nil)
  r.SetPathValue("hash",hash)
  query:=r.URL.Query()
  query.Set("path",file.Path)
  r.URL.RawQuery=query.Encode()
  w:=httptest.NewRecorder()
  a.streamMedia(w,r)
  if w.Code!=200{t.Fatalf("stream %s: %d %s",file.Path,w.Code,w.Body.String())}
  if strings.HasSuffix(file.Path,".vtt") && !strings.Contains(w.Body.String(),"Fixture subtitle"){t.Fatal("subtitle lost")}
 }
 if err:=restarted.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 for _,file:=range movie.library.Files{
  if exists(filepath.Join(restarted.libraryDir,file.Stored)){t.Fatalf("delete left %s",file.Stored)}
 }
}
