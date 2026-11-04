package main

import (
 "bufio"
 "database/sql"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"

 "github.com/alexedwards/argon2id"
 _ "modernc.org/sqlite"
)

var pinPattern=regexp.MustCompile("^[0-9]{6}$")
const pinLockDuration = 15 * time.Minute
const maxPinFailures = 5

// SQLite keeps the original password_hash column to migrate existing installations
// without destroying user records. New values always contain a hash of a six-digit PIN.
func ensurePINSchema(db *sql.DB) error {
 if _,err:=db.Exec("CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY CHECK(id=1), password_hash TEXT NOT NULL, pin_enabled INTEGER NOT NULL DEFAULT 0)");err!=nil{return err}
 rows,err:=db.Query("PRAGMA table_info(users)")
 if err!=nil{return err}
 hasPIN:=false
 for rows.Next(){
  var id,notnull,pk int
  var name,typ string
  var defaultValue sql.NullString
  if err:=rows.Scan(&id,&name,&typ,&notnull,&defaultValue,&pk);err!=nil{rows.Close();return err}
  if name=="pin_enabled"{hasPIN=true}
 }
 if err:=rows.Err();err!=nil{rows.Close();return err}
 rows.Close()
 if !hasPIN{
  if _,err:=db.Exec("ALTER TABLE users ADD COLUMN pin_enabled INTEGER NOT NULL DEFAULT 0");err!=nil{return err}
 }
 _,err=db.Exec("CREATE TABLE IF NOT EXISTS pin_attempts (id INTEGER PRIMARY KEY CHECK(id=1), failures INTEGER NOT NULL DEFAULT 0, locked_until INTEGER NOT NULL DEFAULT 0)")
 if err!=nil{return err}
 _,err=db.Exec("INSERT OR IGNORE INTO pin_attempts (id,failures,locked_until) VALUES (1,0,0)")
 return err
}

func pinConfigured(db *sql.DB) bool {
 var enabled int
 return db.QueryRow("SELECT pin_enabled FROM users WHERE id=1").Scan(&enabled)==nil && enabled==1
}

// Called only from an OS administrator's shell, never from an HTTP handler.
// The installer passes the secret via a private stdin pipe, not argv or env.
func pinCommand(args []string) error {
 if len(args)!=1 || (args[0]!="set" && args[0]!="status"){return errors.New("usage: cloudflix pin {status|set}")}
 dir:=env("CLOUDFLIX_DATA_DIR","./data")
 if err:=os.MkdirAll(dir,0700);err!=nil{return err}
 db,err:=sql.Open("sqlite",filepath.Join(dir,"cloudflix.db"))
 if err!=nil{return err}
 defer db.Close()
 db.SetMaxOpenConns(1)
 if _,err=db.Exec("PRAGMA busy_timeout=5000");err!=nil{return err}
 if err:=ensurePINSchema(db);err!=nil{return err}
 if args[0]=="status"{
  if !pinConfigured(db){return errors.New("Cloudflix PIN is not configured")}
  fmt.Println("Cloudflix PIN configured")
  return nil
 }
 reader:=bufio.NewReader(io.LimitReader(os.Stdin,32))
 line,err:=reader.ReadString('\n')
 if err!=nil && err!=io.EOF{return err}
 pin:=strings.TrimSuffix(strings.TrimSuffix(line,"\n"),"\r")
 if !pinPattern.MatchString(pin){return errors.New("PIN must contain exactly six ASCII digits")}
 hash,err:=argon2id.CreateHash(pin,argon2id.DefaultParams)
 if err!=nil{return err}
 tx,err:=db.Begin()
 if err!=nil{return err}
 defer tx.Rollback()
 if _,err=tx.Exec("INSERT INTO users(id,password_hash,pin_enabled) VALUES(1,?,1) ON CONFLICT(id) DO UPDATE SET password_hash=excluded.password_hash,pin_enabled=1",hash);err!=nil{return err}
 if _,err=tx.Exec("DELETE FROM sessions");err!=nil && !strings.Contains(err.Error(),"no such table"){return err}
 if _,err=tx.Exec("UPDATE pin_attempts SET failures=0,locked_until=0 WHERE id=1");err!=nil{return err}
 if err=tx.Commit();err!=nil{return err}
 fmt.Println("Cloudflix access PIN configured; existing sessions revoked")
 return nil
}

func (a *application) pinLocked() (bool,int64,error){
 var failures int
 var until int64
 if err:=a.db.QueryRow("SELECT failures,locked_until FROM pin_attempts WHERE id=1").Scan(&failures,&until);err!=nil{return false,0,err}
 now:=time.Now().Unix()
 return until>now,until, nil
}

func (a *application) pinFailure() error {
 var failures int
 var until int64
 if err:=a.db.QueryRow("SELECT failures,locked_until FROM pin_attempts WHERE id=1").Scan(&failures,&until);err!=nil{return err}
 now:=time.Now().Unix()
 if until>0 && until<=now{failures=0}
 failures++
 if failures>=maxPinFailures{until=now+int64(pinLockDuration.Seconds())}else{until=0}
 _,err:=a.db.Exec("UPDATE pin_attempts SET failures=?,locked_until=? WHERE id=1",failures,until)
 return err
}

func (a *application) clearPinFailures() error {
 _,err:=a.db.Exec("UPDATE pin_attempts SET failures=0,locked_until=0 WHERE id=1")
 return err
}
