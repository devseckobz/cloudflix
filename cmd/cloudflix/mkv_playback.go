package main

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
 "sync"
 "time"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

const (
 maxMKVFileSize int64 = 24 << 30
 maxConvertedMP4 int64 = 12 << 30
 maxSubtitleOutput int64 = 8 << 20
 maxMKVTracks = 16
)

type mkvTrack struct {
 Index int `json:"index"`
 Language string `json:"language"`
 Label string `json:"label"`
}
type mkvCaption struct {
 Index int `json:"index"`
 Language string `json:"language"`
 Label string `json:"label"`
 URL string `json:"url"`
}
type mkvResult struct {
 State string `json:"state"`
 Message string `json:"message"`
 VideoURL string `json:"videoURL,omitempty"`
 Subtitles []mkvCaption `json:"subtitles"`
}
type mkvJob struct {
 state string
 message string
}
type mkvManager struct {
 mu sync.Mutex
 jobs map[string]*mkvJob
 slot chan struct{}
}
type ffStream struct{
 Index int `json:"index"`
 CodecType string `json:"codec_type"`
 CodecName string `json:"codec_name"`
 PixelFormat string `json:"pix_fmt"`
 Tags struct{
  Language string `json:"language"`
  Title string `json:"title"`
 } `json:"tags"`
}
type ffProbe struct{ Streams []ffStream `json:"streams"` }
var validCaptionLang=regexp.MustCompile("^[a-z]{2,3}(-[a-z]{2})?$")

func newMKVManager()*mkvManager{
 return &mkvManager{jobs:map[string]*mkvJob{},slot:make(chan struct{},1)}
}
func mkvKey(hash,path string)string{
 h:=sha256.Sum256([]byte(hash+"\x00"+path))
 return hex.EncodeToString(h[:])
}
func (a *application) mkvCacheDir()(string,error){
 dir:=filepath.Join(a.dataDir,"playback-cache")
 if err:=os.MkdirAll(dir,0700);err!=nil{return "",err}
 stat,err:=os.Lstat(dir)
 if err!=nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink!=0{return "",errors.New("unsafe playback cache directory")}
 return dir,nil
}
func (a *application) validatedMKVSource(hash,path string)(*os.File,error){
 if !mediaHashPattern.MatchString(hash) || !allowedMediaPath(path) ||
  !strings.EqualFold(filepath.Ext(path),".mkv") || len(path)>4096{
  return nil,errors.New("invalid MKV request")
 }
 e:=a.engine
 e.mu.Lock()
 item:=e.entries[hash]
 permitted:=false
 if item!=nil && item.validated && item.rejectionReason=="" && item.torrent!=nil && item.torrent.Info()!=nil{
  for _,file:=range item.torrent.Files(){
   if filepath.ToSlash(file.Path())==path && file.Length()>0 && verifiedFile(file){
    permitted=true
    break
   }
  }
 }
 e.mu.Unlock()
 if !permitted{return nil,errors.New("MKV playback is available only after download verification")}
 root,err:=os.OpenRoot(e.downloads)
 if err!=nil{return nil,errors.New("movie storage unavailable")}
 defer root.Close()
 file,err:=root.Open(filepath.FromSlash(path))
 if err!=nil{return nil,errors.New("MKV file is unavailable")}
 stat,err:=file.Stat()
 if err!=nil||!stat.Mode().IsRegular()||stat.Size()<=0||stat.Size()>maxMKVFileSize{
  file.Close()
  return nil,errors.New("MKV file is invalid or exceeds the 24 GiB playback limit")
 }
 return file,nil
}
func cachedMKV(dir,key string)([]mkvTrack,bool){
 info,err:=os.Stat(filepath.Join(dir,key+".mp4"))
 if err!=nil || !info.Mode().IsRegular() || info.Size()<=0 {return nil,false}
 raw,err:=os.ReadFile(filepath.Join(dir,key+".json"))
 if err!=nil || len(raw)>16<<10{return nil,false}
 var tracks []mkvTrack
 if json.Unmarshal(raw,&tracks)!=nil || len(tracks)>maxMKVTracks{return nil,false}
 return tracks,true
}
func mkvLanguage(raw string)string{
 lang:=strings.ToLower(strings.TrimSpace(raw))
 alias:=map[string]string{"eng":"en","fra":"fr","fre":"fr","spa":"es","deu":"de","ger":"de","ita":"it","jpn":"ja","kor":"ko","chi":"zh","zho":"zh","dan":"da","fin":"fi","nor":"no","swe":"sv","por":"pt"}
 if mapped,ok:=alias[lang];ok{lang=mapped}
 if !validCaptionLang.MatchString(lang){return "und"}
 return lang
}
func mkvTrackName(raw string,index int)string{
 name:=strings.TrimSpace(raw)
 if name!="" && len(name)<=35 && !strings.ContainsAny(name,"\r\n\x00"){return name}
 return "Subtitle "+strconv.Itoa(index+1)
}
func (a *application) mkvStatusFor(hash,path string)mkvResult{
 result:=mkvResult{State:"idle",Subtitles:[]mkvCaption{}}
 dir,err:=a.mkvCacheDir()
 if err!=nil{return mkvResult{State:"error",Message:"Unable to access the video cache.",Subtitles:[]mkvCaption{}}}
 key:=mkvKey(hash,path)
 if tracks,ready:=cachedMKV(dir,key);ready{
  result.State="ready"
  result.VideoURL="/api/playback/"+hash+"/video?path="+url.QueryEscape(path)
  for _,track:=range tracks{
   result.Subtitles=append(result.Subtitles,mkvCaption{
    Index:track.Index,Language:track.Language,Label:track.Label,
    URL:"/api/playback/"+hash+"/subtitle?path="+url.QueryEscape(path)+"&index="+strconv.Itoa(track.Index),
   })
  }
  return result
 }
 if a.playback==nil{return result}
 a.playback.mu.Lock()
 job:=a.playback.jobs[key]
 if job!=nil{
  result.State=job.state
  result.Message=job.message
 }
 a.playback.mu.Unlock()
 return result
}
func (a *application) mkvStatus(w http.ResponseWriter,r *http.Request){
 file,err:=a.validatedMKVSource(r.PathValue("hash"),r.URL.Query().Get("path"))
 if err!=nil{fail(w,http.StatusNotFound,err.Error());return}
 file.Close()
 w.Header().Set("Cache-Control","no-store")
 respond(w,http.StatusOK,a.mkvStatusFor(r.PathValue("hash"),r.URL.Query().Get("path")))
}
func (a *application) mkvPrepare(w http.ResponseWriter,r *http.Request){
 var request struct {Path string `json:"path"`}
 if !readJSON(w,r,&request){return}
 hash:=r.PathValue("hash")
 source,err:=a.validatedMKVSource(hash,request.Path)
 if err!=nil{fail(w,http.StatusBadRequest,err.Error());return}
 status:=a.mkvStatusFor(hash,request.Path)
 if status.State=="ready" {
  source.Close()
  respond(w,http.StatusOK,status)
  return
 }
 if _,err:=mediatools.Resolve(a.dataDir,"ffmpeg");err!=nil{
  source.Close()
  fail(w,http.StatusServiceUnavailable,"Built-in media tools are unavailable. Check available disk space and update Cloudflix to restore them.")
  return
 }
 if _,err:=mediatools.Resolve(a.dataDir,"ffprobe");err!=nil{
  source.Close()
  fail(w,http.StatusServiceUnavailable,"Built-in media tools are unavailable. Check available disk space and update Cloudflix to restore them.")
  return
 }
 if a.playback==nil{
  source.Close()
  fail(w,http.StatusServiceUnavailable,"MKV transcoder is unavailable")
  return
 }
 key:=mkvKey(hash,request.Path)
 m:=a.playback
 m.mu.Lock()
 if existing:=m.jobs[key];existing!=nil{
  if existing.state!="error"{
   m.mu.Unlock()
   source.Close()
   respond(w,http.StatusAccepted,a.mkvStatusFor(hash,request.Path))
   return
  }
  delete(m.jobs,key) // Permit a new attempt after a recoverable failure.
 }
 if len(m.jobs)>=32{
  m.mu.Unlock()
  source.Close()
  fail(w,http.StatusTooManyRequests,"Too many playback conversions. Try later.")
  return
 }
 job:=&mkvJob{state:"queued",message:"Waiting to prepare your MKV for browser playback"}
 m.jobs[key]=job
 m.mu.Unlock()
 go a.convertMKV(source,hash,request.Path,key,job)
 respond(w,http.StatusAccepted,a.mkvStatusFor(hash,request.Path))
}
func (a *application) updateMKVJob(job *mkvJob,state,message string){
 a.playback.mu.Lock()
 job.state=state
 job.message=message
 a.playback.mu.Unlock()
}
type boundedCommandOutput struct{data bytes.Buffer}
func (w *boundedCommandOutput)Write(p []byte)(int,error){
 if w.data.Len()+len(p)>256<<10{return 0,errors.New("media tool output limit exceeded")}
 return w.data.Write(p)
}
func commandFromMKV(ctx context.Context,exe string,source *os.File,args ...string)([]byte,error){
 // The /proc/self/fd/3 input stays inside an os.Root capability and remains
 // seekable. Reset the shared offset before starting each external tool.
 if _,err:=source.Seek(0,io.SeekStart);err!=nil{return nil,err}
 cmd:=exec.CommandContext(ctx,exe,args...)
 cmd.ExtraFiles=[]*os.File{source}
 cmd.Stderr=io.Discard
 var output boundedCommandOutput
 cmd.Stdout=&output
 err:=cmd.Run()
 return output.data.Bytes(),err
}
func (a *application) convertMKV(source *os.File,hash,path,key string,job *mkvJob){
 defer source.Close()
 m:=a.playback
 select{
 case m.slot<-struct{}{}:
  defer func(){<-m.slot}()
 case <-time.After(10*time.Minute):
  a.updateMKVJob(job,"error","The conversion queue is busy. Please try again.")
  return
 }
 a.updateMKVJob(job,"processing","Preparing a browser-compatible MP4. Large movies may take some time.")
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Hour)
 defer cancel()
 ffmpeg,err:=mediatools.Resolve(a.dataDir,"ffmpeg")
 if err!=nil{a.updateMKVJob(job,"error","Built-in video conversion is unavailable. Check storage and update Cloudflix.");return}
 ffprobe,err:=mediatools.Resolve(a.dataDir,"ffprobe")
 if err!=nil{a.updateMKVJob(job,"error","Built-in video inspection is unavailable. Check storage and update Cloudflix.");return}
 dir,err:=a.mkvCacheDir()
 if err!=nil{a.updateMKVJob(job,"error","Playback cache is unavailable.");return}
 output:=filepath.Join(dir,key+".mp4")
 temporary:=filepath.Join(dir,key+".part.mp4")
 defer os.Remove(temporary)
 probeBytes,err:=commandFromMKV(ctx,ffprobe,source,
  "-v","error","-show_entries","stream=index,codec_type,codec_name,pix_fmt:stream_tags=language,title",
  "-of","json","-i","/proc/self/fd/3")
 if err!=nil{
  a.updateMKVJob(job,"error","Unable to read the MKV container. Check the file's format.")
  return
 }
 var parsed ffProbe
 if json.Unmarshal(probeBytes,&parsed)!=nil{
  a.updateMKVJob(job,"error","The MKV stream information is invalid.")
  return
 }
 hasVideo:=false
 for _,stream:=range parsed.Streams{if stream.CodecType=="video"{hasVideo=true;break}}
 if !hasVideo{
  a.updateMKVJob(job,"error","This MKV contains no video stream.")
  return
 }
 // Fixed arguments only; users cannot inject shell commands or paths.
 // Compatible H.264/AAC streams can be remuxed quickly without re-encoding.
 // HEVC, 10-bit H.264, and other codecs are converted to H.264/AAC.
 copyVideo:=false
 copyAudio:=false
 sawVideo:=false
 sawAudio:=false
 for _,stream:=range parsed.Streams{
  if stream.CodecType=="video" && !sawVideo{
   sawVideo=true
   copyVideo=strings.EqualFold(stream.CodecName,"h264") && strings.EqualFold(stream.PixelFormat,"yuv420p")
  }
  if stream.CodecType=="audio" && !sawAudio{
   sawAudio=true
   copyAudio=strings.EqualFold(stream.CodecName,"aac")
  }
 }
 args:=[]string{"-nostdin","-hide_banner","-loglevel","error","-y",
  "-i","/proc/self/fd/3","-map","0:v:0","-map","0:a:0?","-sn","-dn"}
 if copyVideo{args=append(args,"-c:v","copy")}else{
  args=append(args,"-c:v","libx264","-preset","veryfast","-crf","24","-pix_fmt","yuv420p","-threads","2")
 }
 if copyAudio{args=append(args,"-c:a","copy")}else{
  args=append(args,"-c:a","aac","-b:a","160k")
 }
 args=append(args,"-movflags","+faststart","-fs",strconv.FormatInt(maxConvertedMP4,10),temporary)
 if _,err:=commandFromMKV(ctx,ffmpeg,source,args...);err!=nil{
  a.updateMKVJob(job,"error","MKV conversion failed. Check available disk space and whether the movie file is valid.")
  return
 }
 converted,err:=os.Stat(temporary)
 if err!=nil||!converted.Mode().IsRegular()||converted.Size()==0||converted.Size()>maxConvertedMP4{
  a.updateMKVJob(job,"error","Converted video is unavailable or exceeds the storage limit.")
  return
 }
 tracks:=make([]mkvTrack,0)
 for _,stream:=range parsed.Streams{
  if stream.CodecType!="subtitle"||len(tracks)>=maxMKVTracks{continue}
  switch strings.ToLower(stream.CodecName){
  case "subrip","ass","ssa","webvtt","mov_text","text":
  default:continue // Image-based PGS/DVD subtitles cannot become WebVTT text.
  }
  index:=stream.Index
  if index<0||index>200{continue}
  filename:=filepath.Join(dir,key+"."+strconv.Itoa(index)+".vtt")
  temp:=filename+".part"
  _,err:=commandFromMKV(ctx,ffmpeg,source,"-nostdin","-hide_banner","-loglevel","error",
   "-y","-i","/proc/self/fd/3","-map","0:"+strconv.Itoa(index),"-c:s","webvtt","-f","webvtt",temp)
  if err!=nil{os.Remove(temp);continue}
  stat,err:=os.Stat(temp)
  if err!=nil||stat.Size()>maxSubtitleOutput||stat.Size()==0{os.Remove(temp);continue}
  if err:=os.Rename(temp,filename);err!=nil{os.Remove(temp);continue}
  tracks=append(tracks,mkvTrack{Index:index,Language:mkvLanguage(stream.Tags.Language),Label:mkvTrackName(stream.Tags.Title,len(tracks))})
 }
 if err:=os.Rename(temporary,output);err!=nil{
  a.updateMKVJob(job,"error","Unable to store converted MKV playback.")
  return
 }
 metadata,err:=json.Marshal(tracks)
 if err!=nil{a.updateMKVJob(job,"error","Unable to save subtitle information.");return}
 manifest:=filepath.Join(dir,key+".json")
 tempManifest:=manifest+".part"
 if err:=os.WriteFile(tempManifest,metadata,0600);err!=nil{
  a.updateMKVJob(job,"error","Unable to save conversion metadata.")
  return
 }
 if err:=os.Rename(tempManifest,manifest);err!=nil{
  os.Remove(tempManifest)
  a.updateMKVJob(job,"error","Unable to complete playback preparation.")
  return
 }
 a.updateMKVJob(job,"ready","")
}
func (a *application) serveMKVCache(w http.ResponseWriter,r *http.Request,subtitle bool){
 hash:=r.PathValue("hash")
 path:=r.URL.Query().Get("path")
 source,err:=a.validatedMKVSource(hash,path)
 if err!=nil{fail(w,http.StatusNotFound,"movie is unavailable");return}
 source.Close()
 status:=a.mkvStatusFor(hash,path)
 if status.State!="ready"{fail(w,http.StatusNotFound,"movie is still being prepared");return}
 key:=mkvKey(hash,path)
 filename:=key+".mp4"
 contentType:="video/mp4"
 if subtitle{
  index,err:=strconv.Atoi(r.URL.Query().Get("index"))
  if err!=nil||index<0{fail(w,http.StatusBadRequest,"invalid subtitle index");return}
  authorized:=false
  for _,track:=range status.Subtitles{if track.Index==index{authorized=true;break}}
  if !authorized{fail(w,http.StatusNotFound,"subtitle unavailable");return}
  filename=key+"."+strconv.Itoa(index)+".vtt"
  contentType="text/vtt; charset=utf-8"
 }
 dir,err:=a.mkvCacheDir()
 if err!=nil{fail(w,http.StatusInternalServerError,"playback unavailable");return}
 root,err:=os.OpenRoot(dir)
 if err!=nil{fail(w,http.StatusInternalServerError,"playback unavailable");return}
 defer root.Close()
 file,err:=root.Open(filename)
 if err!=nil{fail(w,http.StatusNotFound,"playback file not found");return}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular(){
  fail(w,http.StatusNotFound,"playback file not found")
  return
 }
 w.Header().Set("Content-Type",contentType)
 w.Header().Set("Content-Disposition","inline")
 w.Header().Set("Cache-Control","private, no-store")
 http.ServeContent(w,r,filename,info.ModTime(),file)
}
