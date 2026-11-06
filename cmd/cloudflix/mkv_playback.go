package main

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "log"
 "net/http"
 "net/url"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "runtime"
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
 ctx context.Context
 cancel context.CancelFunc
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
type ffProbe struct{
 Streams []ffStream `json:"streams"`
 Format struct{Duration string `json:"duration"`} `json:"format"`
}
var validCaptionLang=regexp.MustCompile("^[a-z]{2,3}(-[a-z]{2})?$")

func newMKVManager()*mkvManager{
 return &mkvManager{jobs:map[string]*mkvJob{},slot:make(chan struct{},1)}
}
// transcodeThreads gives libx264 a real share of the machine without letting a
// conversion monopolize every core: the HTTP server, the torrent client and
// the media range handlers still have to answer inside a frame budget while a
// movie is being prepared. The old hard-coded 2 left a 16-core box mostly idle
// and made a feature-length transcode take hours.
func transcodeThreads()int{
 threads:=runtime.NumCPU()/2
 if threads<2{threads=2}
 if threads>8{threads=8}
 return threads
}
func mkvKey(hash,path string)string{
 h:=sha256.Sum256([]byte(hash+"\x00"+path))
 return hex.EncodeToString(h[:])
}

// prewarmClient marks a conversion that has no client at all. It is a sentinel
// rather than an empty string because an empty user agent is a real thing (curl,
// a stripped header) and must keep its own conservative meaning.
const prewarmClient="cloudflix-prewarm"

// clientNeedsFaststart reports whether this client must get a plain faststart
// MP4 rather than a fragmented one.
//
// A fragmented MP4 keeps its sample tables in per-fragment moof boxes, so the
// browser can start playing after a few KB instead of fetching a multi-megabyte
// moov index. That is a large win on a slow link (measured: a 2 h movie's index
// is 4-5 MB, and at 800 kbps the faststart file had not rendered a frame within
// 50 s while the fragmented one started in 0.1 s). WebKit's fMP4 support is the
// fragile case, so Safari keeps the universally-compatible layout: the same file
// it gets today.
//
// Only a positively identified Chromium or Gecko gets fragmented video, because
// a broken player is far worse than a slower start.
func clientNeedsFaststart(userAgent string)bool{
 if userAgent==prewarmClient{return false}
 agent:=strings.ToLower(userAgent)
 // Every browser on iOS and iPadOS renders with WebKit whatever brand token it
 // carries, so CriOS, FxiOS, EdgiOS and OPiOS are Safari's engine underneath.
 for _,webkit:=range []string{"crios","fxios","edgios","opios","iphone","ipad","ipod"}{
  if strings.Contains(agent,webkit){return true}
 }
 if strings.Contains(agent,"chrome/")||strings.Contains(agent,"chromium/"){return false}
 if strings.Contains(agent,"firefox/")&&strings.Contains(agent,"gecko/"){return false}
 return true // Safari, an unknown agent, or no agent at all.
}

// faststartSuffix marks the cache entry that holds the unfragmented layout.
const faststartSuffix=".faststart"

// playbackKey is the cache key for a prepared video in the format this client
// needs. Both layouts coexist so a Safari request is served without waiting for
// a re-encode of the Chromium version.
func playbackKey(hash,path,userAgent string)string{
 key:=mkvKey(hash,path)
 if clientNeedsFaststart(userAgent){return key+faststartSuffix}
 return key
}

// playbackMovflags picks the container layout for a cache key. The faststart
// branch is byte-for-byte what every client got before fragmented support
// existed, so an unrecognised player keeps exactly the old file.
func playbackMovflags(key string)string{
 if strings.HasSuffix(key,faststartSuffix){return "+faststart"}
 return "frag_keyframe+empty_moov+default_base_moof"
}

// mp4Layout inspects the first top-level boxes of a cached MP4 to learn how it
// is laid out. It reports ok=false when the file cannot be read as MP4 at all,
// in which case the caller keeps its previous behaviour instead of forcing a
// conversion of a file it merely failed to understand.
func mp4Layout(file *os.File) (fragmented bool,ok bool){
 header:=make([]byte,8)
 var offset int64
 // Both formats can begin with moov. The first media box tells them apart.
 for i:=0;i<64;i++{
  if _,err:=file.ReadAt(header,offset);err!=nil{return false,false}
  size:=int64(header[0])<<24|int64(header[1])<<16|int64(header[2])<<8|int64(header[3])
  switch string(header[4:8]){
  case "moof":return true,true
  case "mdat":return false,true
  }
  if size<8{return false,false}
  offset+=size
 }
 return false,false
}
func (a *application) mkvCacheDir()(string,error){
 dir:=filepath.Join(a.dataDir,"playback-cache")
 if err:=os.MkdirAll(dir,0700);err!=nil{return "",err}
 stat,err:=os.Lstat(dir)
 if err!=nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink!=0{return "",errors.New("unsafe playback cache directory")}
 return dir,nil
}

func (a *application) permanentPlayback()bool{return a.engine!=nil && a.engine.libraryDir!=""}

func (a *application) playbackOutputDir()(string,error){
 if !a.permanentPlayback(){return a.mkvCacheDir()}
 dir:=a.engine.libraryDir
 return dir,os.MkdirAll(dir,0700)
}

func (a *application) outputKey(hash,path,userAgent string)string{
 if a.permanentPlayback(){return mkvKey(hash,path)}
 return playbackKey(hash,path,userAgent)
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
  permitted=e.fileVerifiedLocked(item,item.torrent,path)
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
// cachedIsFragmented reports whether the cached MP4 for key is fragmented. The
// second result is false when the file does not parse as MP4.
func cachedIsFragmented(dir,key string)(bool,bool){
 file,err:=os.Open(filepath.Join(dir,key+".mp4"))
 if err!=nil{return false,false}
 defer file.Close()
 return mp4Layout(file)
}

// readyLayout reports the tracks of a cached video when its bytes match the
// layout the client needs. baseKey is the plain mkvKey, not a playbackKey: the
// layout suffix is applied here.
//
// Entries written before fragmentation existed hold a faststart MP4 under the
// plain key. Such an entry is moved into the faststart slot, so WebKit viewers
// keep playing immediately while the plain key is freed for the fragmented file
// Chromium is about to request. A file whose layout cannot be determined is
// trusted as-is: it is better to play a possibly-old layout than to re-encode a
// movie on every request.
func readyLayout(dir,baseKey string,wantsFaststart bool)[]mkvTrack{
 key:=baseKey
 if wantsFaststart{key=baseKey+faststartSuffix}
 tracks,found:=cachedMKV(dir,key)
 if !found{
  // A WebKit viewer may be served a legacy faststart entry that still sits
  // under the plain key: adopt it rather than re-encode a playable movie.
  if !wantsFaststart{return nil}
  legacy,ok:=cachedMKV(dir,baseKey)
  if !ok{return nil}
  if fragmented,known:=cachedIsFragmented(dir,baseKey);known&&fragmented{return nil}
  if !adoptLegacyFaststart(dir,baseKey,legacy){return nil}
  if moved,ok:=cachedMKV(dir,key);ok{return moved}
  return nil
 }
 if fragmented,known:=cachedIsFragmented(dir,key);known&&fragmented!=!wantsFaststart{
  // The file under this key has the wrong layout. Adopt it as the WebKit copy
  // if it is a legacy faststart entry, then report "not ready" for the layout
  // actually requested.
  if !wantsFaststart{adoptLegacyFaststart(dir,baseKey,tracks)}
  return nil
 }
 return tracks
}

// adoptLegacyFaststart moves a cache entry written before fragmentation support
// (a faststart MP4 stored under the plain key) into the faststart slot, which
// frees the plain key to hold the fragmented layout without making a WebKit
// viewer re-encode a movie it can already play.
func adoptLegacyFaststart(dir,key string,tracks []mkvTrack)bool{
 legacy:=key+faststartSuffix
 if _,err:=os.Stat(filepath.Join(dir,legacy+".mp4"));err==nil{return false}
 if err:=os.Rename(filepath.Join(dir,key+".mp4"),filepath.Join(dir,legacy+".mp4"));err!=nil{return false}
 for _,track:=range tracks{
  old:=filepath.Join(dir,key+"."+strconv.Itoa(track.Index)+".vtt")
  _=os.Rename(old,filepath.Join(dir,legacy+"."+strconv.Itoa(track.Index)+".vtt"))
 }
 // The manifest goes last: a half-moved entry then reads as "not ready"
 // instead of "ready" with subtitles missing.
 return os.Rename(filepath.Join(dir,key+".json"),filepath.Join(dir,legacy+".json"))==nil
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
func (a *application) mkvStatusFor(hash,path,userAgent string)mkvResult{
 result:=mkvResult{State:"idle",Subtitles:[]mkvCaption{}}
 dir,err:=a.playbackOutputDir()
 if err!=nil{return mkvResult{State:"error",Message:"Unable to access the video cache.",Subtitles:[]mkvCaption{}}}
 key:=mkvKey(hash,path)
 var tracks []mkvTrack
 if a.permanentPlayback(){
  prepared,ok:=readPreparedLibrary(dir,key)
  info,err:=os.Lstat(filepath.Join(a.engine.downloads,filepath.FromSlash(path)))
  if ok && prepared.Source.Path==path && err==nil && prepared.Source.matches(info){
   if prepared.hasOutputs(dir,key){
    tracks=prepared.Tracks
   }
  }
 }else{tracks=readyLayout(dir,key,clientNeedsFaststart(userAgent))}
 if tracks!=nil{
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
 job:=a.playback.jobs[a.outputKey(hash,path,userAgent)]
 if job!=nil && job.state!="ready"{
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
 respond(w,http.StatusOK,a.mkvStatusFor(r.PathValue("hash"),r.URL.Query().Get("path"),r.UserAgent()))
}
// startPrepare validates a source and queues its conversion, returning the
// status to report. It is shared by the HTTP handler and the background
// prewarm that runs when a download completes.
func (a *application) startPrepare(hash,path,userAgent string)(mkvResult,error){
 source,err:=a.validatedMKVSource(hash,path)
 if err!=nil{return mkvResult{},err}
 if status:=a.mkvStatusFor(hash,path,userAgent);status.State=="ready"{
  source.Close()
  return status,nil
 }
 if _,err:=mediatools.Resolve(a.dataDir,"ffmpeg");err!=nil{
  source.Close()
  return mkvResult{},errors.New("Built-in media tools are unavailable. Check available disk space and update Cloudflix to restore them.")
 }
 if _,err:=mediatools.Resolve(a.dataDir,"ffprobe");err!=nil{
  source.Close()
  return mkvResult{},errors.New("Built-in media tools are unavailable. Check available disk space and update Cloudflix to restore them.")
 }
 if a.playback==nil{
  source.Close()
  return mkvResult{},errors.New("MKV transcoder is unavailable")
 }
 key:=a.outputKey(hash,path,userAgent)
 m:=a.playback
 if a.permanentPlayback(){
  e:=a.engine
  e.mu.Lock()
  defer e.mu.Unlock()
  item:=e.entries[hash]
  info,err:=source.Stat()
  current,currentErr:=os.Lstat(filepath.Join(e.downloads,filepath.FromSlash(path)))
  if item==nil || item.torrent==nil || err!=nil || currentErr!=nil || !os.SameFile(info,current){
   source.Close()
   return mkvResult{},errors.New("The source MKV is no longer queued.")
  }
 }
 m.mu.Lock()
 if existing:=m.jobs[key];existing!=nil{
  if existing.state=="queued" || existing.state=="processing"{
   m.mu.Unlock()
   source.Close()
   return a.mkvStatusFor(hash,path,userAgent),nil
  }
  delete(m.jobs,key) // Permit a new attempt after a recoverable failure.
 }
 active:=0
 for _,job:=range m.jobs{if job.state=="queued" || job.state=="processing"{active++}}
 if active>=32{
  m.mu.Unlock()
  source.Close()
  return mkvResult{},errors.New("Too many playback conversions. Try later.")
 }
 ctx,cancel:=context.WithCancel(context.Background())
 job:=&mkvJob{state:"queued",message:"Waiting to prepare your MKV for browser playback",ctx:ctx,cancel:cancel}
 m.jobs[key]=job
 m.mu.Unlock()
 go a.convertMKV(source,hash,path,key,job)
 return a.mkvStatusFor(hash,path,userAgent),nil
}

// prepareInBackground is the engine's completion hook: it converts a finished
// MKV without waiting for anyone to click Watch, so the movie is playable as
// soon as the download ends. Failures are deliberately silent — a movie that
// cannot be prepared will still report its state when the user opens it.
func (a *application) prepareInBackground(hash,path string){
 // Production prepares the same permanent MP4 for every browser.
 if _,err:=a.startPrepare(hash,path,prewarmClient);err!=nil{
  log.Printf("Playback prewarm skipped for %s: %v",hash,err)
 }
}

func (a *application) mkvPrepare(w http.ResponseWriter,r *http.Request){
 var request struct {Path string `json:"path"`}
 if !readJSON(w,r,&request){return}
 status,err:=a.startPrepare(r.PathValue("hash"),request.Path,r.UserAgent())
 if err!=nil{
  if strings.Contains(err.Error(),"Too many playback conversions"){
   fail(w,http.StatusTooManyRequests,err.Error())
   return
  }
  if a.playback==nil{
   fail(w,http.StatusServiceUnavailable,err.Error())
   return
  }
  fail(w,http.StatusBadRequest,err.Error())
  return
 }
 if status.State=="ready"{
  respond(w,http.StatusOK,status)
  return
 }
 respond(w,http.StatusAccepted,status)
}
func (a *application) updateMKVJob(job *mkvJob,state,message string){
 a.playback.mu.Lock()
 job.state=state
 job.message=message
 a.playback.mu.Unlock()
}
// boundedCommandOutput keeps a media tool's stdout from growing without bound.
// It truncates instead of failing: returning an error here would abort the
// whole conversion, and a legitimate long encode must never be killed by a
// diagnostic cap. ffmpeg's stderr is discarded, so this only bounds ffprobe's
// JSON, which is parsed and rejected cleanly if it was cut short.
type boundedCommandOutput struct{data bytes.Buffer}
func (w *boundedCommandOutput)Write(p []byte)(int,error){
 const limit=256<<10
 if room:=limit-w.data.Len();room<=0{return len(p),nil}else if len(p)>room{
  w.data.Write(p[:room])
  return len(p),nil
 }
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
// Both layouts use the same codecs. A prepared sibling only needs remuxing,
// even if the original MKV needed a full encode.
func remuxCachedLayout(ctx context.Context,ffmpeg,dir,key,output string)bool{
 base:=strings.TrimSuffix(key,faststartSuffix)
 sibling:=base+faststartSuffix
 if sibling==key{sibling=base}
 if _,ready:=cachedMKV(dir,sibling);!ready{return false}
 file,err:=os.Open(filepath.Join(dir,sibling+".mp4"))
 if err!=nil{return false}
 defer file.Close()
 if _,ok:=mp4Layout(file);!ok{return false}
 _,err=commandFromMKV(ctx,ffmpeg,file,"-nostdin","-hide_banner","-loglevel","error","-xerror","-y",
  "-i","/proc/self/fd/3","-map","0:v:0","-map","0:a:0?","-c","copy",
  "-movflags",playbackMovflags(key),"-fs",strconv.FormatInt(maxConvertedMP4,10),output)
 return err==nil
}

func (a *application) convertMKV(source *os.File,hash,path,key string,job *mkvJob){
 defer source.Close()
 m:=a.playback
 jobCtx:=job.ctx
 if jobCtx==nil{jobCtx=context.Background()}
 if job.cancel!=nil{defer job.cancel()}
 select{
 case m.slot<-struct{}{}:
  defer func(){<-m.slot}()
 case <-time.After(10*time.Minute):
  a.updateMKVJob(job,"error","The conversion queue is busy. Please try again.")
  return
 case <-jobCtx.Done():
  a.updateMKVJob(job,"error","Playback preparation was cancelled.")
  return
 }
 a.updateMKVJob(job,"processing","Preparing a browser-compatible MP4. Large movies may take some time.")
 ctx,cancel:=context.WithTimeout(jobCtx,3*time.Hour)
 defer cancel()
 ffmpeg,err:=mediatools.Resolve(a.dataDir,"ffmpeg")
 if err!=nil{a.updateMKVJob(job,"error","Built-in video conversion is unavailable. Check storage and update Cloudflix.");return}
 ffprobe,err:=mediatools.Resolve(a.dataDir,"ffprobe")
 if err!=nil{a.updateMKVJob(job,"error","Built-in video inspection is unavailable. Check storage and update Cloudflix.");return}
 dir,err:=a.playbackOutputDir()
 if err!=nil{a.updateMKVJob(job,"error","Playback cache is unavailable.");return}
 output:=filepath.Join(dir,key+".mp4")
 temporary:=filepath.Join(dir,key+".part.mp4")
 defer os.Remove(temporary)
 permanent:=a.permanentPlayback()
 limit:=maxConvertedMP4
 var unpublished []string
 published:=false
 var original libraryOriginal
 if permanent{
  info,err:=source.Stat()
  if err!=nil{a.updateMKVJob(job,"error","Unable to inspect the source MKV.");return}
  original=libraryOriginal{Path:path,Size:info.Size(),Modified:info.ModTime().UnixNano()}
  defer func(){
   if !published{for _,path:=range unpublished{_ = os.Remove(path)}}
  }()
  // These older layouts can be regenerated from the still-verified source.
  // Reclaim them before requiring space for the new permanent MP4.
  a.engine.mu.Lock()
  if item:=a.engine.entries[hash];item!=nil{a.engine.deletePlaybackCacheLocked(item,[]string{path})}
  a.engine.mu.Unlock()
  storage:=a.engine.storage()
  _,free,err:=filesystemSpace(dir)
  if !storage.Known || err!=nil || min(storage.Available,free)<16<<20{
   a.updateMKVJob(job,"error","Not enough space to convert this MKV. The original was kept.")
   return
  }
  limit=int64(min(uint64(limit),min(storage.Available,free)-(8<<20)))
 }
 probeBytes,err:=commandFromMKV(ctx,ffprobe,source,
  "-v","error","-show_entries","stream=index,codec_type,codec_name,pix_fmt:stream_tags=language,title:format=duration",
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
  args=append(args,"-c:v","libx264","-preset","veryfast","-crf","24","-pix_fmt","yuv420p","-threads",strconv.Itoa(transcodeThreads()))
 }
 if copyAudio{args=append(args,"-c:a","copy")}else{
  args=append(args,"-c:a","aac","-b:a","160k")
 }
 flags:=playbackMovflags(key)
 if permanent{flags="+faststart";args=append([]string{"-xerror"},args...)}
 args=append(args,"-movflags",flags,"-fs",strconv.FormatInt(limit,10),temporary)
 if permanent || !remuxCachedLayout(ctx,ffmpeg,dir,key,temporary){
  if _,err:=commandFromMKV(ctx,ffmpeg,source,args...);err!=nil{
   a.updateMKVJob(job,"error","MKV conversion failed. Check available disk space and whether the movie file is valid.")
   return
  }
 }
 converted,err:=os.Stat(temporary)
 if err!=nil||!converted.Mode().IsRegular()||converted.Size()==0||converted.Size()>=limit{
  a.updateMKVJob(job,"error","Converted video is unavailable or exceeds the storage limit.")
  return
 }
 if permanent && !verifyLibraryMP4(ctx,ffprobe,temporary,parsed.Format.Duration){
  a.updateMKVJob(job,"error","Converted MP4 failed verification. The original MKV was kept.")
  return
 }
 tracks:=make([]mkvTrack,0)
 for _,stream:=range parsed.Streams{
  if stream.CodecType!="subtitle"{continue}
  switch strings.ToLower(stream.CodecName){
  case "subrip","ass","ssa","webvtt","mov_text","text":
  default:continue // Image-based PGS/DVD subtitles cannot become WebVTT text.
  }
  index:=stream.Index
  if len(tracks)>=maxMKVTracks || index<0||index>200{
   if permanent{a.updateMKVJob(job,"error","Too many subtitle tracks to preserve. The original MKV was kept.");return}
   continue
  }
  filename:=filepath.Join(dir,key+"."+strconv.Itoa(index)+".vtt")
  temp:=filename+".part"
  _,err:=commandFromMKV(ctx,ffmpeg,source,"-nostdin","-hide_banner","-loglevel","error",
   "-y","-i","/proc/self/fd/3","-map","0:"+strconv.Itoa(index),"-c:s","webvtt","-f","webvtt",temp)
  if err!=nil{
   os.Remove(temp)
   if permanent{a.updateMKVJob(job,"error","Subtitle extraction failed. The original MKV was kept.");return}
   continue
  }
  stat,err:=os.Stat(temp)
  if err==nil && (stat.Size()>maxSubtitleOutput||stat.Size()==0){err=errors.New("invalid subtitle size")}
  if err==nil && permanent{err=syncLibraryFile(temp)}
  if err==nil{err=os.Rename(temp,filename)}
  if err!=nil{
   os.Remove(temp)
   if permanent{a.updateMKVJob(job,"error","Unable to save a subtitle. The original MKV was kept.");return}
   continue
  }
  tracks=append(tracks,mkvTrack{Index:index,Language:mkvLanguage(stream.Tags.Language),Label:mkvTrackName(stream.Tags.Title,len(tracks))})
  if permanent{unpublished=append(unpublished,filename)}
 }
 if permanent && syncLibraryFile(temporary)!=nil{
  a.updateMKVJob(job,"error","Unable to sync the converted MP4. The original MKV was kept.");return
 }
 publishingLocked:=false
 if permanent{
  a.engine.mu.Lock()
  publishingLocked=true
  defer func(){if publishingLocked{a.engine.mu.Unlock()}}()
  if item:=a.engine.entries[hash];ctx.Err()!=nil || item==nil || item.torrent==nil{
   a.updateMKVJob(job,"error","Playback preparation was cancelled.")
   return
  }
 }
 if err:=os.Rename(temporary,output);err!=nil{
  a.updateMKVJob(job,"error","Unable to store converted MKV playback.")
  return
 }
 if permanent{unpublished=append(unpublished,output)}
 var record any=tracks
 if permanent{
  info,err:=source.Stat()
  if err!=nil || !original.matches(info) || ctx.Err()!=nil{
   a.updateMKVJob(job,"error","The source MKV changed or preparation was cancelled.")
   return
  }
  record=preparedLibraryFile{Source:original,Tracks:tracks}
 }
 metadata,err:=json.Marshal(record)
 if err!=nil{a.updateMKVJob(job,"error","Unable to save subtitle information.");return}
 manifest:=filepath.Join(dir,key+".json")
 tempManifest:=manifest+".part"
 if err:=os.WriteFile(tempManifest,metadata,0600);err!=nil{
  a.updateMKVJob(job,"error","Unable to save conversion metadata.")
  return
 }
 if permanent && syncLibraryFile(tempManifest)!=nil{
  _=os.Remove(tempManifest)
  a.updateMKVJob(job,"error","Unable to sync the conversion metadata.");return
 }
 if err:=os.Rename(tempManifest,manifest);err!=nil{
  os.Remove(tempManifest)
  a.updateMKVJob(job,"error","Unable to complete playback preparation.")
  return
 }
 published=true
 if publishingLocked{
  a.engine.mu.Unlock()
  publishingLocked=false
 }
 a.updateMKVJob(job,"ready","")
 if permanent{
  m.mu.Lock()
  if m.jobs[key]==job{delete(m.jobs,key)}
  m.mu.Unlock()
  a.engine.finalizeLibrary()
 }
}
func (a *application) serveMKVCache(w http.ResponseWriter,r *http.Request,subtitle bool){
 hash:=r.PathValue("hash")
 path:=r.URL.Query().Get("path")
 source,err:=a.validatedMKVSource(hash,path)
 if err!=nil{fail(w,http.StatusNotFound,"movie is unavailable");return}
 source.Close()
 status:=a.mkvStatusFor(hash,path,r.UserAgent())
 if status.State!="ready"{fail(w,http.StatusNotFound,"movie is still being prepared");return}
 key:=a.outputKey(hash,path,r.UserAgent())
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
 dir,err:=a.playbackOutputDir()
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
 if subtitle{
  w.Header().Set("Cache-Control","private, no-store")
  http.ServeContent(w,r,filename,info.ModTime(),file)
  return
 }
 // The converted file is written once and renamed into place, so a strong
 // validator lets the player reuse buffered ranges across seeks instead of
 // re-downloading them. Still private to the session and revalidated on use.
 serveVideoBody(w,r,filename,info.Size(),info.ModTime(),file)
}
