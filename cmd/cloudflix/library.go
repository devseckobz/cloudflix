package main

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "log"
 "math"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
)

type libraryFile struct{
 Path string
 Size int64
 Stored string
}

type libraryMovie struct{
 Files []libraryFile
 Owned []string
 Originals []libraryOriginal
}

type libraryOriginal struct{
 Path string
 Size int64
 Modified int64
}

func (original libraryOriginal) matches(info os.FileInfo)bool{
 return info.Mode().IsRegular() && info.Size()==original.Size && info.ModTime().UnixNano()==original.Modified
}

type preparedLibraryFile struct{
 Source libraryOriginal
 Tracks []mkvTrack
}

func readPreparedLibrary(dir,key string)(preparedLibraryFile,bool){
 var prepared preparedLibraryFile
 raw,err:=os.ReadFile(filepath.Join(dir,key+".json"))
 if err!=nil || len(raw)>16<<10 || json.Unmarshal(raw,&prepared)!=nil ||
  prepared.Source.Path=="" || prepared.Source.Size<=0 || prepared.Tracks==nil || len(prepared.Tracks)>maxMKVTracks{
  return prepared,false
 }
 return prepared,true
}

func (prepared preparedLibraryFile) hasOutputs(dir,key string)bool{
 valid:=func(name string)bool{
  info,err:=os.Stat(filepath.Join(dir,name))
  return err==nil && info.Mode().IsRegular() && info.Size()>0
 }
 if !valid(key+".mp4"){return false}
 for _,track:=range prepared.Tracks{
  if !valid(key+"."+strconv.Itoa(track.Index)+".vtt"){return false}
 }
 return true
}

func ensureLibrarySchema(db *sql.DB)error{
 var count int
 if err:=db.QueryRow("SELECT count(*) FROM pragma_table_info('torrents') WHERE name='library'").Scan(&count);err!=nil{return err}
 if count!=0{return nil}
 _,err:=db.Exec("ALTER TABLE torrents ADD COLUMN library TEXT NOT NULL DEFAULT ''")
 return err
}

func (m *libraryMovie) file(path string)(libraryFile,bool){
 if m!=nil{
  for _,file:=range m.Files{if file.Path==path{return file,true}}
 }
 return libraryFile{},false
}

func (e *engine) saveLibraryLocked(hash string,movie *libraryMovie)error{
 raw,err:=json.Marshal(movie)
 if err!=nil{return err}
 result,err:=e.db.Exec("UPDATE torrents SET library=? WHERE hash=?",string(raw),hash)
 if err!=nil{return err}
 rows,err:=result.RowsAffected()
 if err!=nil{return err}
 if rows!=1{return fmt.Errorf("library row missing for %s",hash)}
 return nil
}

func syncLibraryFile(path string)error{
 file,err:=os.Open(path)
 if err!=nil{return err}
 defer file.Close()
 return file.Sync()
}

func verifyLibraryMP4(ctx context.Context,ffprobe,path,sourceDuration string)bool{
 original,err:=strconv.ParseFloat(sourceDuration,64)
 if err!=nil || original<=0 || math.IsInf(original,0) || math.IsNaN(original){return false}
 file,err:=os.Open(path)
 if err!=nil{return false}
 defer file.Close()
 if fragmented,known:=mp4Layout(file);!known || fragmented{return false}
 cmd:=exec.CommandContext(ctx,ffprobe,"-v","error","-err_detect","explode","-count_frames",
  "-show_entries","stream=codec_type,codec_name,nb_read_frames:format=duration","-of","json","-i","/proc/self/fd/3")
 cmd.ExtraFiles=[]*os.File{file}
 var output,diagnostic boundedCommandOutput
 cmd.Stdout,cmd.Stderr=&output,&diagnostic
 if err:=cmd.Run();err!=nil || diagnostic.data.Len()!=0{return false}
 var report struct{
  Streams []struct{
   Type string `json:"codec_type"`
   Codec string `json:"codec_name"`
   Frames string `json:"nb_read_frames"`
  } `json:"streams"`
  Format struct{Duration string `json:"duration"`} `json:"format"`
 }
 if json.Unmarshal(output.data.Bytes(),&report)!=nil{return false}
 duration,err:=strconv.ParseFloat(report.Format.Duration,64)
 if err!=nil || !(math.Abs(original-duration)<=max(0.2,min(2,original*0.05))){return false}
 video:=false
 for _,stream:=range report.Streams{
  frames,_:=strconv.ParseInt(stream.Frames,10,64)
  if frames<=0{return false}
  if stream.Type=="video" && stream.Codec=="h264"{video=true
  }else if stream.Type!="audio" || stream.Codec!="aac"{return false}
 }
 return video
}

// Publish a durable catalog before dropping the torrent and unlinking any
// source. On restart a published movie never re-adds its original magnet.
func (e *engine) finalizeLibrary(){
 if e.libraryDir==""{return}
 e.mu.Lock()
 defer e.mu.Unlock()
 for _,item:=range e.entries{
  if item.library==nil{
   movie:=e.preparedLibraryLocked(item)
   if movie==nil{continue}
   err:=syncLibraryFile(e.libraryDir)
   if err==nil{err=syncLibraryFile(filepath.Dir(e.libraryDir))}
   if err!=nil{
    log.Printf("Could not sync converted movie %s: %v",item.hash,err)
    continue
   }
   if err:=e.saveLibraryLocked(item.hash,movie);err!=nil{
    log.Printf("Could not publish converted movie %s: %v",item.hash,err)
    continue
   }
   item.library=movie
   item.torrent.DisallowDataDownload()
   item.torrent.Drop()
   item.torrent=nil
   item.fileManifest=nil
  }
  e.removeLibraryOriginalsLocked(item)
 }
}

func (e *engine) preparedLibraryLocked(item *entry)*libraryMovie{
 if item.torrent==nil || item.paused || !item.validated || item.rejectionReason!=""{return nil}
 tt:=item.torrent
 if tt.Info()==nil{return nil}
 movie:=&libraryMovie{}
 for _,file:=range tt.Files(){
  path:=filepath.ToSlash(file.Path())
  if !allowedMediaPath(path) || file.Length()<=0 || !strings.EqualFold(filepath.Ext(path),".mkv"){continue}
  key:=mkvKey(item.hash,path)
  prepared,ready:=readPreparedLibrary(e.libraryDir,key)
  if !ready || prepared.Source.Path!=path{return nil}
  info,err:=os.Lstat(filepath.Join(e.downloads,filepath.FromSlash(path)))
  if err!=nil || !prepared.Source.matches(info){return nil}
  dir:="converted/"+key+"/"
  stem:=strings.TrimSuffix(filepath.Base(path),filepath.Ext(path))
  if !appendLibraryFile(movie,e.libraryDir,dir+stem+".mp4",key+".mp4"){return nil}
  for _,track:=range prepared.Tracks{
   index:=strconv.Itoa(track.Index)
   if !appendLibraryFile(movie,e.libraryDir,dir+stem+"."+index+"."+mkvLanguage(track.Language)+".vtt",key+"."+index+".vtt"){return nil}
  }
  movie.Originals=append(movie.Originals,prepared.Source)
 }
 if len(movie.Originals)==0{return nil}
 // Only check download completion once every MKV output is ready. MP4-only
 // rows and unfinished conversions never need progress snapshots here.
 for _,file:=range tt.Files(){
  path:=filepath.ToSlash(file.Path())
  if !allowedMediaPath(path) || file.Length()<=0{continue}
  if !e.fileVerifiedLocked(item,tt,path){return nil}
  if !strings.EqualFold(filepath.Ext(path),".mkv"){movie.Files=append(movie.Files,libraryFile{Path:path,Size:file.Length()})}
 }
 movie.Owned=e.torrentAllFilesLocked(item)
 return movie
}

func appendLibraryFile(movie *libraryMovie,dir,path,stored string)bool{
 info,err:=os.Stat(filepath.Join(dir,stored))
 if err!=nil || !info.Mode().IsRegular() || info.Size()<=0{return false}
 movie.Files=append(movie.Files,libraryFile{Path:path,Size:info.Size(),Stored:stored})
 return true
}

func (e *engine) removeLibraryOriginalsLocked(item *entry){
 movie:=item.library
 if len(movie.Originals)==0{return}
 for _,file:=range movie.Files{
  if file.Stored==""{continue}
  info,err:=os.Stat(filepath.Join(e.libraryDir,file.Stored))
  if err!=nil || !info.Mode().IsRegular() || info.Size()!=file.Size{return}
 }
 // An unresolved magnet may share an on-disk path with this movie.
 for _,other:=range e.entries{
  if other.torrent!=nil && other.torrent.Info()==nil && other.rejectionReason==""{return}
 }
 // Force the committed library row out of WAL before unlinking its source.
 var busy,frames,checkpointed int
 if err:=e.db.QueryRow("PRAGMA wal_checkpoint(FULL)").Scan(&busy,&frames,&checkpointed);err!=nil || busy!=0 || checkpointed<frames{return}
 root,err:=os.OpenRoot(e.downloads)
 if err!=nil{return}
 defer root.Close()
 originals:=make([]string,0,len(movie.Originals))
 replaced:=make(map[string]bool,len(movie.Originals))
 for _,source:=range movie.Originals{
  if !safeTorrentPath(source.Path){return}
  info,err:=root.Lstat(filepath.FromSlash(source.Path))
  if err!=nil && !os.IsNotExist(err){return}
  if err==nil && source.matches(info){originals=append(originals,source.Path)}
  replaced[source.Path]=true
 }
 if err:=e.deleteTorrentFilesLocked(item,originals);err!=nil{
  log.Printf("Could not remove converted MKV for %s: %v",item.hash,err)
  return
 }
 next:=*movie
 next.Originals=nil
 next.Owned=nil
 for _,path:=range movie.Owned{if !replaced[path]{next.Owned=append(next.Owned,path)}}
 if err:=e.saveLibraryLocked(item.hash,&next);err!=nil{
  log.Printf("Could not record removal of converted MKV for %s: %v",item.hash,err)
  return
 }
 item.library=&next
}

func (e *engine) removeLibraryFilesLocked(item *entry,owned []string)error{
 if e.libraryDir==""{return nil}
 keys:=map[string]bool{}
 if item.library!=nil{
  for _,file:=range item.library.Files{
   if file.Stored!=""{keys[strings.SplitN(file.Stored,".",2)[0]]=true}
  }
 }else{
  for _,path:=range owned{
   if strings.EqualFold(filepath.Ext(path),".mkv"){keys[mkvKey(item.hash,path)]=true}
  }
 }
 if len(keys)==0{return nil}
 root,err:=os.OpenRoot(e.libraryDir)
 if os.IsNotExist(err){return nil}
 if err!=nil{return err}
 defer root.Close()
 entries,err:=os.ReadDir(e.libraryDir)
 if err!=nil{return err}
 for _,entry:=range entries{
  match:=playbackArtifact.FindStringSubmatch(entry.Name())
  if match==nil || !keys[match[1]] || !entry.Type().IsRegular(){continue}
  if err:=root.Remove(entry.Name());err!=nil && !os.IsNotExist(err){return fmt.Errorf("remove converted movie: %w",err)}
 }
 return nil
}

func (e *engine) cancelPlaybackLocked(item *entry,owned []string){
 if e.playback==nil{return}
 e.playback.mu.Lock()
 defer e.playback.mu.Unlock()
 for _,path:=range owned{
  if !strings.EqualFold(filepath.Ext(path),".mkv"){continue}
  key:=mkvKey(item.hash,path)
  for _,variant:=range []string{key,key+faststartSuffix}{
   if job:=e.playback.jobs[variant];job!=nil{
    if job.cancel!=nil{job.cancel()}
    delete(e.playback.jobs,variant)
   }
  }
 }
}
