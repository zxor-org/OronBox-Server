package server

import (
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminHomeResourceOptions(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminResources(r.Context(), store.AdminResourceQuery{Search: r.URL.Query().Get("q"), Moderation: "visible", CurrentRevisionState: "approved", Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: 25, Sort: "updated_desc"})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("resource_options_failed", err.Error()))
		return
	}
	items := make([]map[string]string, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]string{"id": item.ID, "name": item.Name, "slug": item.Slug, "owner": item.Owner})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "total": page.Total, "total_pages": page.TotalPages})
}

func (a *App) handleAdminHomeSelectorScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(adminHomeSelectorJS))
}

const adminHomeSelectorJS = `(function(){
document.querySelectorAll('select[name="resource_id"]').forEach(function(select){
 var box=document.createElement('div');box.className='async-selector';
 var input=document.createElement('input');input.type='search';input.placeholder='搜索全部资源';input.setAttribute('aria-label','搜索全部资源');
 var status=document.createElement('span');status.className='cell-note';status.setAttribute('aria-live','polite');
 var prev=document.createElement('button');prev.type='button';prev.textContent='上一页';
 var next=document.createElement('button');next.type='button';next.textContent='下一页';
 box.append(input,prev,next,status);select.parentNode.insertBefore(box,select);box.appendChild(select);
 var page=1,totalPages=1,timer,requestNo=0;
 async function load(target){var request=++requestNo;status.textContent='加载中';prev.disabled=true;next.disabled=true;try{var response=await fetch('/admin/home/resource-options?q='+encodeURIComponent(input.value)+'&page='+target);if(!response.ok)throw new Error('request');var data=await response.json();if(request!==requestNo)return;var selected=select.value;select.replaceChildren(new Option('选择资源',''));data.items.forEach(function(item){select.add(new Option(item.name+' · '+item.slug+' · '+item.owner,item.id));});if(selected&&!Array.from(select.options).some(function(option){return option.value===selected;})){select.add(new Option('当前选择 · '+selected,selected));}select.value=selected;page=data.page;totalPages=data.total_pages||1;prev.disabled=page<=1;next.disabled=page>=totalPages;status.textContent='第 '+page+'/'+totalPages+' 页，共 '+data.total+' 项';}catch(_){if(request===requestNo)status.textContent='加载失败，请重试';}finally{if(request===requestNo){prev.disabled=page<=1;next.disabled=page>=totalPages;}}}
 input.addEventListener('input',function(){clearTimeout(timer);timer=setTimeout(function(){load(1);},250);});prev.addEventListener('click',function(){if(page>1)load(page-1);});next.addEventListener('click',function(){if(page<totalPages)load(page+1);});load(1);
});})();`
