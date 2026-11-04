package main

import (
 "math"
 "strings"
 "testing"
)

func TestMovieRatingSchemaMigratesExistingDatabase(t *testing.T){
 db:=testPINDB(t)
 _,err:=db.Exec("CREATE TABLE torrents (hash TEXT PRIMARY KEY, magnet TEXT NOT NULL, poster TEXT NOT NULL DEFAULT '', looked_up INTEGER NOT NULL DEFAULT 0)")
 if err!=nil{t.Fatal(err)}
 if _,err=db.Exec("INSERT INTO torrents(hash,magnet,poster,looked_up) VALUES('abc','magnet:?xt=urn:btih:abc','/movie.jpg',1)");err!=nil{t.Fatal(err)}
 if err:=ensureMovieRatingSchema(db);err!=nil{t.Fatal(err)}
 if err:=ensureMovieRatingSchema(db);err!=nil{t.Fatalf("second migration must be idempotent: %v",err)}
 var hash,poster string
 var lookedUp int
 var rating float64
 if err:=db.QueryRow("SELECT hash,poster,looked_up,rating FROM torrents WHERE hash='abc'").Scan(&hash,&poster,&lookedUp,&rating);err!=nil{t.Fatal(err)}
 if hash!="abc" || poster!="/movie.jpg" || lookedUp!=1 || rating!=0{t.Fatal("existing metadata was not preserved")}
 if _,err=db.Exec("UPDATE torrents SET rating=7.6 WHERE hash='abc'");err!=nil{t.Fatal(err)}
 if err:=db.QueryRow("SELECT rating FROM torrents WHERE hash='abc'").Scan(&rating);err!=nil{t.Fatal(err)}
 if math.Abs(rating-7.6)>0.001{t.Fatal("new rating column did not persist")}
}

func TestTMDBMovieParser(t *testing.T){
 cases:=[]struct{body string;title,year,poster string;rating float64}{
  {`{"results":[{"title":"Big Buck Bunny","release_date":"2008-04-10","poster_path":"/bunny.jpg","backdrop_path":"/bunny-wide.jpg","vote_average":7.83}]}`,"Big Buck Bunny","2008","/bunny.jpg",7.83},
  {`{"results":[{"title":"Untitled","release_date":"","vote_average":0}]}`,"Untitled","2020","",0},
  {`{"results":[{"title":"No Rating","release_date":"2018-01-01","vote_average":10.8}]}`,"No Rating","2018","",0},
  {`{"results":[]}`,"","","",0},
 }
 for _,tc:=range cases{
  title,year,poster,_,rating,_,err:=parseTMDBMovie(strings.NewReader(tc.body),"2020")
  if err!=nil{t.Fatal(err)}
  if title!=tc.title||year!=tc.year||poster!=tc.poster||math.Abs(rating-tc.rating)>0.001{
   t.Errorf("got %q %q %q %.2f, want %q %q %q %.2f",title,year,poster,rating,tc.title,tc.year,tc.poster,tc.rating)
  }
 }
}
