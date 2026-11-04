package main

import (
 "strings"
 "testing"
)

func TestMovieGenreMapping(t *testing.T){
 cases:=[]struct{ids []int;want string}{
  {[]int{27,53},"Horror"},
  {[]int{28,12},"Action"},
  {[]int{35,10749},"Comedy"},
  {[]int{878,28},"Sci-Fi"},
  {[]int{99},"Documentary"},
  {[]int{999999,18},"Drama"},
  {[]int{999999},""},
  {nil,""},
 }
 for _,tc:=range cases{
  got:=firstMovieGenre(tc.ids)
  if got!=tc.want{t.Errorf("genres %v: got %q, want %q",tc.ids,got,tc.want)}
 }
}

func TestTMDBSearchParsesFirstMovieGenre(t *testing.T){
 input:=`{"results":[{"title":"Example","release_date":"2026-07-21","poster_path":"/poster.jpg","backdrop_path":"/backdrop.jpg","vote_average":7.5,"genre_ids":[27,53]}]}`
 title,year,poster,backdrop,rating,genre,err:=parseTMDBMovie(strings.NewReader(input),"2025")
 if err!=nil{t.Fatal(err)}
 if title!="Example"||year!="2026"||poster!="/poster.jpg"||backdrop!="/backdrop.jpg"||rating!=7.5||genre!="Horror"{
  t.Fatalf("parsed unexpected TMDB movie: %s %s %s %s %.1f %s",title,year,poster,backdrop,rating,genre)
 }
 title,_,_,_,_,genre,err=parseTMDBMovie(strings.NewReader(`{"results":[{"title":"Unknown genre","genre_ids":[888888]}]}`),"")
 if err!=nil||title!="Unknown genre"||genre!=""{t.Fatalf("should not invent unknown genres: %s %s %v",title,genre,err)}
}

func TestGenreMigrationPreservesExistingMovieMetadata(t *testing.T){
 db:=testPINDB(t)
 _,err:=db.Exec(`CREATE TABLE torrents(hash TEXT PRIMARY KEY,magnet TEXT NOT NULL,rejection_reason TEXT NOT NULL DEFAULT '',title TEXT NOT NULL DEFAULT '',year TEXT NOT NULL DEFAULT '',poster TEXT NOT NULL DEFAULT '',looked_up INTEGER NOT NULL DEFAULT 0)`)
 if err!=nil{t.Fatal(err)}
 if _,err=db.Exec(`INSERT INTO torrents(hash,magnet,title,year,poster,looked_up) VALUES('movie','magnet:?xt=urn:btih:movie','A Movie','2026','/poster.jpg',1)`);err!=nil{t.Fatal(err)}
 if _,err=db.Exec(`INSERT INTO torrents(hash,magnet,rejection_reason,looked_up) VALUES('rejected','magnet:?xt=urn:btih:rejected','not MP4',1)`);err!=nil{t.Fatal(err)}
 if err:=ensureMovieGenreSchema(db);err!=nil{t.Fatal(err)}
 var title,year,poster,genre string
 var lookedUp int
 if err:=db.QueryRow("SELECT title,year,poster,genre,looked_up FROM torrents WHERE hash='movie'").Scan(&title,&year,&poster,&genre,&lookedUp);err!=nil{t.Fatal(err)}
 if title!="A Movie"||year!="2026"||poster!="/poster.jpg"||genre!=""||lookedUp!=0{t.Fatalf("migration lost original metadata or failed to queue backfill: %q %q %q %q %d",title,year,poster,genre,lookedUp)}
 if err:=db.QueryRow("SELECT looked_up FROM torrents WHERE hash='rejected'").Scan(&lookedUp);err!=nil{t.Fatal(err)}
 if lookedUp!=1{t.Fatal("rejected magnets must not be backfilled")}
 if _,err:=db.Exec("UPDATE torrents SET genre='Horror',looked_up=1 WHERE hash='movie'");err!=nil{t.Fatal(err)}
 if err:=ensureMovieGenreSchema(db);err!=nil{t.Fatal(err)}
 if err:=db.QueryRow("SELECT genre,looked_up FROM torrents WHERE hash='movie'").Scan(&genre,&lookedUp);err!=nil{t.Fatal(err)}
 if genre!="Horror"||lookedUp!=1{t.Fatal("idempotent migration must not clear genre or repeat TMDB lookup")}
}
