package main

import (
 "reflect"
 "testing"
)

func TestQueueOrderStableAcrossRepeatedSnapshots(t *testing.T){
 // Go maps intentionally randomize iteration. A UI receiving this snapshot
 // every two seconds must never see cards change places on their own.
 items:=map[string]*entry{
  "c":{hash:"c",queuePosition:3,title:"Third"},
  "a":{hash:"a",queuePosition:1,title:"First"},
  "b":{hash:"b",queuePosition:2,title:"Second"},
 }
 engine:=&engine{entries:items}
 want:=[]string{"a","b","c"}
 for i:=0;i<100;i++{
  snap:=engine.snapshot()
  got:=make([]string,0,len(snap))
  for _,item:=range snap{got=append(got,item["hash"].(string))}
  if !reflect.DeepEqual(got,want){t.Fatalf("snapshot %d: got %v, expected %v",i,got,want)}
 }
 items["d"]=&entry{hash:"d",queuePosition:4}
 got:=orderedEntries(items)
 if len(got)!=4 || got[3].hash!="d"{t.Fatalf("newly added movie must appear last: %+v",got)}
 delete(items,"b")
 got=orderedEntries(items)
 if len(got)!=3 || got[0].hash!="a" || got[1].hash!="c"{t.Fatal("removing one item must not reorder other movies")}
}

func TestQueueStableTieBreaksByHash(t *testing.T){
 items:=map[string]*entry{
  "bbb":{hash:"bbb",queuePosition:0},
  "ccc":{hash:"ccc",queuePosition:2},
  "aaa":{hash:"aaa",queuePosition:0},
  "ddd":{hash:"ddd",queuePosition:2},
 }
 sorted:=orderedEntries(items)
 got:=make([]string,len(sorted))
 for i,x:=range sorted{got[i]=x.hash}
 want:=[]string{"ccc","ddd","aaa","bbb"}
 if !reflect.DeepEqual(got,want){t.Fatalf("expected deterministic ties: %v",got)}
}

func TestSQLiteRowidKeepsTorrentAddOrderAfterRestart(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,magnet TEXT NOT NULL)");err!=nil{t.Fatal(err)}
 for _,hash:=range []string{"z","a","m"}{
  if _,err:=db.Exec("INSERT INTO torrents(hash,magnet) VALUES(?,?)",hash,"magnet:?xt=urn:btih:"+hash);err!=nil{t.Fatal(err)}
 }
 load:=func()map[string]*entry{
  rows,err:=db.Query("SELECT rowid,hash FROM torrents ORDER BY rowid ASC")
  if err!=nil{t.Fatal(err)}
  defer rows.Close()
  out:=map[string]*entry{}
  for rows.Next(){
   var rowid int64
   var hash string
   if err:=rows.Scan(&rowid,&hash);err!=nil{t.Fatal(err)}
   out[hash]=&entry{hash:hash,queuePosition:rowid}
  }
  if err:=rows.Err();err!=nil{t.Fatal(err)}
  return out
 }
 expect:=[]string{"z","a","m"}
 for iteration:=0;iteration<3;iteration++{
  items:=orderedEntries(load())
  got:=make([]string,0,len(items))
  for _,item:=range items{got=append(got,item.hash)}
  if !reflect.DeepEqual(got,expect){t.Fatalf("restore %d: got %v",iteration,got)}
 }
}
