package web

import (
	"net/url"
	"strconv"
)

// Pagination is the shared view model for every paginated admin list.
type Pagination struct {
	BasePath   string
	Query      url.Values
	Page       int
	PerPage    int
	Total      int
	TotalPages int
	From       int
	To         int
	Pages      []int
	PageParam  string
	SizeParam  string
}

func NewPagination(basePath string, rawQuery url.Values, page, perPage, total int) Pagination {
	return NewNamedPagination(basePath, rawQuery, page, perPage, total, "page", "per_page")
}

func NewNamedPagination(basePath string, rawQuery url.Values, page, perPage, total int, pageParam, sizeParam string) Pagination {
	if pageParam == "" {
		pageParam = "page"
	}
	if sizeParam == "" {
		sizeParam = "per_page"
	}
	if perPage < 1 {
		perPage = 25
	}
	if perPage > 100 {
		perPage = 100
	}
	if total < 0 {
		total = 0
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	if page < 1 {
		page = 1
	}
	if totalPages == 0 {
		page = 1
	}
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}

	query := cloneValues(rawQuery)
	query.Del(pageParam)
	query.Del(sizeParam)
	pager := Pagination{
		BasePath: basePath, Query: query, Page: page, PerPage: perPage,
		Total: total, TotalPages: totalPages, PageParam: pageParam, SizeParam: sizeParam,
	}
	if total > 0 {
		pager.From = (page-1)*perPage + 1
		pager.To = page * perPage
		if pager.To > total {
			pager.To = total
		}
	}
	pager.Pages = pageWindow(page, totalPages, 5)
	return pager
}

func (p Pagination) URL(page int) string {
	if page < 1 {
		page = 1
	}
	if p.TotalPages > 0 && page > p.TotalPages {
		page = p.TotalPages
	}
	query := cloneValues(p.Query)
	query.Set(p.PageParam, strconv.Itoa(page))
	query.Set(p.SizeParam, strconv.Itoa(p.PerPage))
	if encoded := query.Encode(); encoded != "" {
		return p.BasePath + "?" + encoded
	}
	return p.BasePath
}

func (p Pagination) PerPageURL(perPage int) string {
	if perPage != 25 && perPage != 50 && perPage != 100 {
		perPage = 25
	}
	query := cloneValues(p.Query)
	query.Set(p.PageParam, "1")
	query.Set(p.SizeParam, strconv.Itoa(perPage))
	return p.BasePath + "?" + query.Encode()
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, entries := range values {
		cloned[key] = append([]string(nil), entries...)
	}
	return cloned
}

func pageWindow(current, total, size int) []int {
	if total < 1 || size < 1 {
		return nil
	}
	if size > total {
		size = total
	}
	start := current - size/2
	if start < 1 {
		start = 1
	}
	if start+size-1 > total {
		start = total - size + 1
	}
	pages := make([]int, 0, size)
	for page := start; page < start+size; page++ {
		pages = append(pages, page)
	}
	return pages
}
