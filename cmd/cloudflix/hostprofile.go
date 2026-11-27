package main

// The transcoder has to size itself to the machine it lands on: a 1 GB / 2-core
// VPS must not start a 4K encode that needs 1.4 GB, and a 32-core box must not
// convert a movie with 8 threads when 16 are sitting idle. Everything here is
// read from the host at conversion time, so one binary behaves correctly on
// every server without configuration.

import (
 "bufio"
 "os"
 "runtime"
 "strconv"
 "strings"
 "sync"
 "time"
)

// hostProfile is what the transcoder is allowed to use. Zero fields mean
// "unknown", and the policy falls back to the conservative choice.
type hostProfile struct {
 Cores int
 // TotalBytes is the memory ceiling this process may not exceed: the cgroup
 // limit when one is set (containers, systemd MemoryMax), otherwise MemTotal.
 // A cgroup limit is what actually kills an allocation, so it takes priority.
 TotalBytes uint64
 // AvailableBytes is what is free right now: MemAvailable, capped by the
 // cgroup headroom. It is sampled per conversion, not once at startup.
 AvailableBytes uint64
}

// memory limits that mean "no limit" in cgroup files.
const unlimitedMemory = ^uint64(0)

var (
 hostProfileMu sync.Mutex
 hostProfileAt time.Time
 hostProfileCache hostProfile
)

// hostMemoryProfile samples the machine, reusing a reading for a few seconds so
// a burst of conversions does not re-read sysfs for every job.
func hostMemoryProfile()hostProfile{
 hostProfileMu.Lock()
 defer hostProfileMu.Unlock()
 if !hostProfileAt.IsZero() && time.Since(hostProfileAt)<5*time.Second{
  return hostProfileCache
 }
 profile:=hostProfile{Cores:runtime.NumCPU()}
 profile.TotalBytes,profile.AvailableBytes=readHostMemory()
 hostProfileCache,hostProfileAt=profile,time.Now()
 return profile
}

// readHostMemory returns (ceiling, available) for this process.
func readHostMemory()(total,available uint64){
 total=readMeminfoKey("MemTotal")
 available=readMeminfoKey("MemAvailable")
 // cgroup v2 then v1: the limit, not the host's RAM, is what OOM-kills us.
 if limit:=cgroupLimit(); limit>0 && limit<unlimitedMemory{
  if total==0 || limit<total{total=limit}
 }
 // Usage of the cgroup counts this process too, so headroom is limit-used.
 if used:=cgroupUsage(); used>0 && total>0{
  headroom:=uint64(0)
  if used<total{headroom=total-used}
  if available==0 || headroom<available{available=headroom}
 }
 if available>total{available=total}
 return total,available
}

func readMeminfoKey(key string)uint64{
 file,err:=os.Open("/proc/meminfo")
 if err!=nil{return 0}
 defer file.Close()
 scanner:=bufio.NewScanner(file)
 for scanner.Scan(){
  line:=scanner.Text()
  if !strings.HasPrefix(line,key+":"){continue}
  fields:=strings.Fields(strings.TrimPrefix(line,key+":"))
  if len(fields)==0{return 0}
  value,err:=strconv.ParseUint(fields[0],10,64)
  if err!=nil{return 0}
  return value<<10 // /proc/meminfo is in kB
 }
 return 0
}

func readUintFile(path string)(uint64,bool){
 raw,err:=os.ReadFile(path)
 if err!=nil{return 0,false}
 value,err:=strconv.ParseUint(strings.TrimSpace(string(raw)),10,64)
 if err!=nil{return 0,false}
 return value,true
}

// cgroupLimit returns the tightest memory ceiling applied to this process, or 0
// when there is none (or it is the "unlimited" sentinel).
func cgroupLimit()uint64{
 var best uint64
 for _,path:=range []string{
  "/sys/fs/cgroup/memory.max",                    // cgroup v2
  "/sys/fs/cgroup/memory/memory.limit_in_bytes",  // cgroup v1
 }{
  if value,ok:=readUintFile(path); ok && value>0 && value<unlimitedMemory{
   if best==0 || value<best{best=value}
  }
 }
 return best
}

func cgroupUsage()uint64{
 for _,path:=range []string{
  "/sys/fs/cgroup/memory.current",
  "/sys/fs/cgroup/memory/memory.usage_in_bytes",
 }{
  if value,ok:=readUintFile(path); ok{return value}
 }
 return 0
}
