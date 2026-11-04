package main

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestReleaseVersionComparison(t *testing.T){
 tests:=[]struct{installed,latest string;available bool}{
  {"v0.1.7","v0.1.8",true},
  {"v0.1.8","v0.1.8",false},
  {"v0.1.9","v0.1.8",false},
  {"v0.9.99","v1.0.0",true},
  {"v1.10.0","v1.9.99",false},
  {"v0.1.7","invalid",false},
  {"dev","v0.1.8",false},
  {"v0.1.8-alpha","v0.1.9",false},
  {"v0.1.8","v0.1.8-alpha",false},
 }
 for _,test:=range tests{
  if got:=newerVersion(test.installed,test.latest);got!=test.available{
   t.Errorf("%s -> %s: got %v, want %v",test.installed,test.latest,got,test.available)
  }
 }
}

func TestWebUpdaterCannotAcceptRemoteCommandsOrURLs(t *testing.T){
 a:=&application{dataDir:t.TempDir()}
 for _,body:=range []string{
  `{"url":"https://evil.example/update.sh"}`,
  `{"command":"rm -rf /"}`,
  `{"version":"v9.9.9"}`,
 }{
  req:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1/api/updates",strings.NewReader(body))
  req.Header.Set("Content-Type","application/json")
  response:=httptest.NewRecorder()
  a.requestUpdate(response,req)
  if response.Code!=http.StatusBadRequest{
   t.Errorf("Unexpected response to remote command payload %s: HTTP %d",body,response.Code)
  }
 }
}

func TestUpdateRequestPathIsFixedInsideApplicationData(t *testing.T){
 dir:=t.TempDir()
 a:=&application{dataDir:dir}
 path:=a.updateRequestPath()
 if !strings.HasPrefix(path,dir) || !strings.HasSuffix(path,"update.request"){
  t.Fatalf("Unexpected update request path: %s",path)
 }
}
