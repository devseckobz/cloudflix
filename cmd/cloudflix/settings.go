package main

import (
 "context"
 "database/sql"
 "errors"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

func ensureSettingsSchema(db *sql.DB)error{
 _,err:=db.Exec("CREATE TABLE IF NOT EXISTS app_settings (name TEXT PRIMARY KEY, value TEXT NOT NULL)")
 return err
}

func loadTMDBKey(db *sql.DB, fallback string)(string,error){
 var key string
 err:=db.QueryRow("SELECT value FROM app_settings WHERE name='tmdb_api_key'").Scan(&key)
 if errors.Is(err,sql.ErrNoRows){return fallback,nil}
 return key,err
}

func (a *application) getSettings(w http.ResponseWriter,r *http.Request){
 a.engine.mu.Lock()
 configured:=a.engine.tmdbKey!=""
 a.engine.mu.Unlock()
 respond(w,http.StatusOK,map[string]any{"tmdbConfigured":configured})
}

// TMDB supports both a 32-character v3 API key and a v4 API Read
// Access Token. Credentials remain server-side and are never returned.
func isTMDBV3Key(key string)bool{
 if len(key)!=32{return false}
 for _,ch:=range key {
  if !strings.ContainsRune("0123456789abcdefABCDEF",ch){return false}
 }
 return true
}
func validTMDBToken(key string)bool{
 if isTMDBV3Key(key){return true}
 if len(key)<32 || len(key)>2048{return false}
 for _,ch:=range key{
  if !(ch>='a'&&ch<='z' || ch>='A'&&ch<='Z' || ch>='0'&&ch<='9' || ch=='_' || ch=='-' || ch=='.'){return false}
 }
 return true
}

func applyTMDBAuthentication(req *http.Request,key string){
 if isTMDBV3Key(key){
  q:=req.URL.Query()
  q.Set("api_key",key)
  req.URL.RawQuery=q.Encode()
 }else{
  req.Header.Set("Authorization","Bearer "+key)
 }
}

func validateTMDBBearer(ctx context.Context,key string)error{
 if !validTMDBToken(key){return errors.New("enter a valid TMDB v3 API key or API Read Access Token")}
 reqCtx,cancel:=context.WithTimeout(ctx,10*time.Second)
 defer cancel()
 req,err:=http.NewRequestWithContext(reqCtx,"GET","https://api.themoviedb.org/3/authentication",nil)
 if err!=nil{return errors.New("cannot prepare TMDB verification")}
 applyTMDBAuthentication(req,key)
 req.Header.Set("Accept","application/json")
 client:=&http.Client{
  Timeout:10*time.Second,
  CheckRedirect:func(req *http.Request,via []*http.Request)error{return http.ErrUseLastResponse},
 }
 response,err:=client.Do(req)
 if err!=nil{return errors.New("TMDB verification unavailable; try again")}
 defer response.Body.Close()
 _,_=io.Copy(io.Discard,io.LimitReader(response.Body,4096))
 if response.StatusCode==http.StatusUnauthorized || response.StatusCode==http.StatusForbidden{
  return errors.New("TMDB rejected this credential")
 }
 if response.StatusCode!=http.StatusOK{
  return errors.New("TMDB verification unavailable; try again")
 }
 return nil
}

func (a *application) saveSettings(w http.ResponseWriter,r *http.Request){
 var req struct{
  TMDBAPIKey string `json:"tmdbApiKey"`
 }
 if !readJSON(w,r,&req){return}
 key:=strings.TrimSpace(req.TMDBAPIKey)
 if key!=""{
  if err:=validateTMDBBearer(r.Context(),key);err!=nil{
   fail(w,http.StatusBadRequest,err.Error());return
  }
 }
 a.engine.mu.Lock()
 defer a.engine.mu.Unlock()
 if _,err:=a.db.ExecContext(r.Context(),"INSERT INTO app_settings(name,value) VALUES('tmdb_api_key',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value",key);err!=nil{
  fail(w,http.StatusInternalServerError,"Unable to save TMDB settings");return
 }
 if a.engine.tmdbKey!=key{
  a.engine.tmdbKey=key
  for _,item:=range a.engine.entries{
   if key!="" && item.poster==""{
    item.lookedUp=false
    item.retryAt=time.Time{}
    item.attempts=0
   }
  }
  if key!=""{_,_=a.db.ExecContext(r.Context(),"UPDATE torrents SET looked_up=0 WHERE poster=''")}
 }
 a.tmdbKey=key
 respond(w,http.StatusOK,map[string]any{"saved":true,"tmdbConfigured":key!=""})
}

// Limit outbound validation calls to a fixed TMDB HTTPS host. Never accept
// a user-provided URL as a credential verification endpoint.
var _ = url.Values{}
