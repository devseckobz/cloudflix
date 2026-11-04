/**
 * Return completed, searchable, stable movie pages.
 * @template {{hash: string, title: string, year: string, status: string}} T
 * @param {T[]} movies
 * @param {string} search
 * @param {number} requestedPage
 * @param {number} [pageSize]
 */
export function selectLibraryPage(movies,search,requestedPage,pageSize=10){
 const query=search.trim().toLocaleLowerCase()
 const filtered=movies.filter(m=>m.status==='completed')
  .filter(m=>!query || m.title.toLocaleLowerCase().includes(query) || m.year.toLocaleLowerCase().includes(query))
  .sort((a,b)=>a.title.localeCompare(b.title,undefined,{sensitivity:'base'})||a.year.localeCompare(b.year)||a.hash.localeCompare(b.hash))
 const total=filtered.length
 const size=Number.isSafeInteger(pageSize) && pageSize>0?pageSize:10
 const totalPages=Math.max(1,Math.ceil(total/size))
 const selected=Number.isFinite(requestedPage)?Math.trunc(requestedPage):1
 const page=Math.max(1,Math.min(totalPages,selected))
 const start=(page-1)*size
 const pagesVisible=Math.min(totalPages,5)
 const firstPage=Math.max(1,Math.min(page-2,totalPages-pagesVisible+1))
 const pages=Array.from({length:pagesVisible},(_,i)=>firstPage+i)
 return {movies:filtered.slice(start,start+size),total,totalPages,page,pages,from:total?start+1:0,to:Math.min(start+size,total)}
}

/** @param {{status: string}[]} movies */
export function countDownloading(movies){
 return movies.filter(m=>m.status==='downloading').length
}
