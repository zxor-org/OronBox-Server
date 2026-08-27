package web

// ThemeJS runs before first paint so a stored theme preference does not flash
// the opposite palette. It stays separate from AdminJS because it must load
// synchronously in the document head.
const ThemeJS = `(() => {
  const savedTheme = localStorage.getItem('oronbox_server_theme');
  if (savedTheme === 'dark' || savedTheme === 'light') {
    document.documentElement.classList.add(savedTheme + '-theme');
  }
})();
`

// TransitionJS drives the automatic redirect on OAuth transition pages. The
// target arrives through a data attribute and is re-checked here so a
// scheme that never passes server-side validation cannot execute either.
const TransitionJS = `(() => {
  const holder = document.querySelector('[data-transition-target]');
  if (!holder) return;
  const target = holder.dataset.transitionTarget;
  if (!target) return;
  if (/^\s*(javascript|data|vbscript):/i.test(target)) return;
  window.setTimeout(() => { location.replace(target); }, 900);
})();
`

// AdminJS carries every behaviour of the admin console. Serving it as a static
// asset instead of inline script tags is what allows the console to run under
// a Content-Security-Policy without 'unsafe-inline'.
const AdminJS = adminCSRFJS + adminShellJS + adminLinkListJS + adminHomeComposerJS + adminBlogJS + adminReviewBulkJS + adminHTMXJS

// adminReviewBulkJS keeps the batch bar honest: it reports how many items are
// selected and refuses to submit a batch that would be rejected server-side
// anyway. The server enforces both rules regardless; this only saves the
// operator a round trip and a lost note. The same bar serves the review queue
// and the comment console, so the wording comes off the page.
const adminReviewBulkJS = `(() => {
  // Bind at document level. Filter and bulk posts replace the queue via HTMX,
  // which would drop listeners attached to the original bar.
  const selected = (form) => {
    const nodes = [...form.querySelectorAll('[data-bulk-item]:checked')];
    if (form.id) {
      document.querySelectorAll('[form="' + form.id + '"][data-bulk-item]:checked').forEach((node) => {
        if (!nodes.includes(node)) nodes.push(node);
      });
    }
    return nodes;
  };
  const refresh = (bar) => {
    const form = bar.closest('form');
    const counter = bar.querySelector('[data-bulk-count]');
    if (!form || !counter) return;
    const count = selected(form).length;
    bar.classList.toggle('has-selection', count > 0);
    counter.textContent = count === 0 ? '未选择' : '已选择 ' + count + ' ' + (bar.dataset.bulkNoun || '项');
  };
  const problem = (bar, form, action) => {
    const note = bar.querySelector('[data-bulk-note]');
    if (selected(form).length === 0) return ['请先勾选要批量处理的' + (bar.dataset.bulkNoun || '项') + '。', null];
    if ((action === 'reject' || action === 'hide') && !note?.value.trim()) {
      return [bar.dataset.bulkNoteError || '这个操作必须填写理由。', note];
    }
    return null;
  };
  const report = (form, message, field) => {
    const summary = form.querySelector('.form-error-summary');
    if (summary) {
      summary.textContent = message;
      summary.hidden = false;
      window.setTimeout(() => summary.focus(), 0);
    }
    field?.setAttribute('aria-invalid', 'true');
    field?.focus();
  };
  document.addEventListener('change', (event) => {
    if (!(event.target instanceof Element) || !event.target.matches('[data-bulk-item]')) return;
    event.target.form?.querySelectorAll('[data-bulk-bar]').forEach(refresh);
  });
  // Capture so an empty or unreasoned batch never reaches the confirm dialog.
  document.addEventListener('click', (event) => {
    const button = event.target.closest('[name="bulk_action"]');
    if (!button || !button.form) return;
    const bar = button.form.querySelector('[data-bulk-bar]');
    if (!bar) return;
    const found = problem(bar, button.form, button.value);
    if (!found) return;
    event.preventDefault();
    event.stopPropagation();
    report(button.form, found[0], found[1]);
  }, true);
  document.addEventListener('submit', (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    const bar = form.querySelector('[data-bulk-bar]');
    if (!bar || event.submitter?.name !== 'bulk_action') return;
    const found = problem(bar, form, event.submitter.value);
    if (!found) return;
    event.preventDefault();
    report(form, found[0], found[1]);
  });
  document.addEventListener('input', (event) => {
    if (event.target instanceof Element && event.target.matches('[data-bulk-note]')) {
      event.target.removeAttribute('aria-invalid');
    }
  });
})();
`

// adminCSRFJS exposes the session token to the few fetch() callers. Form posts
// carry the token as a hidden field instead and never need this.
const adminCSRFJS = `function adminCSRFHeaders() {
  const token = document.querySelector('meta[name="csrf-token"]')?.content;
  return token ? {'X-CSRF-Token': token} : {};
}
`

const adminShellJS = `(() => {
  const root = document.documentElement;
  const path = location.pathname.replace(/\/$/, '') || '/';
  const drawer = document.querySelector('.nav');
  const overlay = document.querySelector('.drawer-overlay');
  const drawerButton = document.querySelector('[data-drawer-toggle]');
  const desktopDrawer = matchMedia('(min-width: 901px)');
  const setDrawer = (open) => {
    drawer?.classList.toggle('open', open);
    overlay?.classList.toggle('open', open);
    document.body.classList.toggle('drawer-open', open);
    drawerButton?.setAttribute('aria-expanded', String(open));
  };
  drawerButton?.addEventListener('click', () => {
    if (desktopDrawer.matches) {
      document.body.classList.toggle('drawer-collapsed');
      drawerButton.setAttribute('aria-expanded', String(!document.body.classList.contains('drawer-collapsed')));
      return;
    }
    setDrawer(!drawer?.classList.contains('open'));
  });
  overlay?.addEventListener('click', () => setDrawer(false));
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') setDrawer(false);
  });

  const liveRegion = document.querySelector('#admin-live-region');
  const announce = (message) => {
    if (!liveRegion) return;
    liveRegion.textContent = '';
    window.setTimeout(() => { liveRegion.textContent = message; }, 20);
  };

  const themeButton = document.querySelector('[data-theme-toggle]');
  const prefersDark = matchMedia('(prefers-color-scheme: dark)');
  const isDark = () => root.classList.contains('dark-theme') ||
    (!root.classList.contains('light-theme') && prefersDark.matches);
  const syncThemeIcon = () => {
    const dark = isDark();
    const icon = themeButton?.querySelector('use');
    if (icon) icon.setAttribute('href', dark ? '#i-light_mode' : '#i-dark_mode');
    themeButton?.setAttribute('aria-label', dark ? '切换到浅色模式' : '切换到深色模式');
  };
  themeButton?.addEventListener('click', () => {
    const next = isDark() ? 'light' : 'dark';
    root.classList.remove('dark-theme', 'light-theme');
    root.classList.add(next + '-theme');
    localStorage.setItem('oronbox_server_theme', next);
    syncThemeIcon();
  });
  syncThemeIcon();

  document.querySelectorAll('[data-nav-path]').forEach((link) => {
    const target = link.dataset.navPath;
    const exact = target === '/admin';
    const aliases = (link.dataset.navAlias || '').split(',').filter(Boolean);
    if ((exact && path === target) || (!exact && path.startsWith(target)) || aliases.some((alias) => path.startsWith(alias))) {
      link.classList.add('active');
      link.setAttribute('aria-current', 'page');
    }
  });

  document.querySelectorAll('[data-copy]').forEach((node) => {
    node.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(node.dataset.copy || node.textContent.trim());
        node.classList.add('copied');
        announce('已复制到剪贴板');
        window.setTimeout(() => node.classList.remove('copied'), 1200);
      } catch (_) { announce('复制失败，请手动复制'); }
    });
  });

  document.querySelectorAll('button:not([type])').forEach((button) => {
    button.type = button.closest('form') ? 'submit' : 'button';
  });
  const iconLabels = { delete: '删除', close: '关闭', arrow_upward: '上移', arrow_downward: '下移', edit: '编辑', content_copy: '复制', download: '下载', refresh: '刷新', first_page: '第一页', last_page: '最后一页' };
  document.querySelectorAll('button, a.icon-button').forEach((control) => {
    const icon = control.querySelector('use');
    const iconName = (icon?.getAttribute('href') || '').replace('#i-', '');
    icon?.setAttribute('aria-hidden', 'true');
    const visibleText = Array.from(control.childNodes).filter((node) => node.nodeType === Node.TEXT_NODE).map((node) => node.textContent).join('').trim();
    if (control.getAttribute('aria-label') || visibleText) return;
    const label = control.getAttribute('title') || iconLabels[iconName] || iconName;
    if (label) control.setAttribute('aria-label', label);
  });
  document.querySelectorAll('input, select, textarea').forEach((field, index) => {
    if (field.type === 'hidden' || field.labels?.length || field.getAttribute('aria-label') || field.getAttribute('aria-labelledby')) return;
    const label = field.closest('label')?.textContent.replace(field.value || '', '').trim() || field.placeholder || field.name;
    if (label) field.setAttribute('aria-label', label);
    if (!field.id) field.id = 'admin-field-' + index;
  });

  document.querySelectorAll('.table-wrap table').forEach((table) => {
    const labels = Array.from(table.querySelectorAll('thead th')).map((cell) => cell.textContent.trim());
    table.querySelectorAll('tbody tr').forEach((row) => {
      Array.from(row.cells).forEach((cell, index) => {
        if (cell.colSpan === 1 && labels[index]) cell.dataset.label = labels[index];
      });
    });
  });

  document.querySelectorAll('form').forEach((form) => {
    const errorSummary = document.createElement('div');
    errorSummary.className = 'form-error-summary';
    errorSummary.setAttribute('role', 'alert');
    errorSummary.setAttribute('tabindex', '-1');
    errorSummary.hidden = true;
    form.prepend(errorSummary);
    form.addEventListener('invalid', (event) => {
      event.target.setAttribute('aria-invalid', 'true');
      errorSummary.textContent = '请检查标出的字段：' + (event.target.labels?.[0]?.textContent.trim() || event.target.getAttribute('aria-label') || '表单字段');
      errorSummary.hidden = false;
      window.setTimeout(() => errorSummary.focus(), 0);
    }, true);
    form.addEventListener('input', (event) => {
      if (event.target.checkValidity()) event.target.removeAttribute('aria-invalid');
      if (form.checkValidity()) errorSummary.hidden = true;
    });
    form.addEventListener('submit', (event) => {
      const submitter = event.submitter;
      if (!submitter) return;
      if (form.dataset.submitting === 'true') {
        event.preventDefault();
        return;
      }
      form.dataset.submitting = 'true';
      submitter.classList.add('submitting');
      submitter.setAttribute('aria-busy', 'true');
      submitter.setAttribute('aria-disabled', 'true');
    });
  });

  document.querySelectorAll('[data-toast]').forEach((toast) => {
    window.setTimeout(() => {
      toast.classList.add('leaving');
      window.setTimeout(() => toast.remove(), 220);
    }, 3000);
  });

  const confirmDialog = document.querySelector('#confirm-dialog');
  const confirmText = confirmDialog?.querySelector('[data-confirm-text]');
  const confirmAction = confirmDialog?.querySelector('[data-confirm-action]');
  let pendingAction = null;
  let dialogTrigger = null;
  let bypassAction = null;
  document.addEventListener('click', (event) => {
    const action = event.target.closest('[data-confirm]');
    if (!action || !confirmDialog) return;
    if (action === bypassAction) {
      bypassAction = null;
      return;
    }
    event.preventDefault();
    pendingAction = action;
    dialogTrigger = action;
    if (confirmText) confirmText.textContent = action.dataset.confirm;
    confirmDialog.showModal();
    window.setTimeout(() => confirmAction?.focus(), 0);
  });
  confirmAction?.addEventListener('click', () => {
    if (!pendingAction) return;
    const action = pendingAction;
    pendingAction = null;
    confirmDialog.close();
    if (action.form) action.form.requestSubmit(action);
    else { bypassAction = action; action.click(); }
  });
  // The reject reason is enforced on the server too; this only saves the
  // reviewer a round trip that would otherwise discard the filled-in form.
  document.addEventListener('click', (event) => {
    const button = event.target.closest('button[name="decision"], button[name="action"]');
    if (!button || !button.form) return;
    const field = button.form.querySelector('[data-required-for]');
    if (!field) return;
    const needed = (field.dataset.requiredFor || '').split(/[\s,]+/);
    if (!needed.includes(button.value)) return;
    if (field.value.trim() !== '') return;
    event.preventDefault();
    field.setCustomValidity(button.value === 'hide' ? '隐藏必须填写理由' : '退回必须填写理由');
    field.reportValidity();
    field.addEventListener('input', () => field.setCustomValidity(''), { once: true });
  }, true);

  document.addEventListener('click', (event) => {
    document.querySelectorAll('details.inline-editor[open]').forEach((details) => {
      if (!details.contains(event.target)) details.removeAttribute('open');
    });
  });

  confirmDialog?.addEventListener('cancel', () => { pendingAction = null; });
  confirmDialog?.addEventListener('close', () => {
    pendingAction = null;
    dialogTrigger?.focus();
    dialogTrigger = null;
  });
})();
`

// adminLinkListJS powers the repeatable link rows shared by the review
// correction form and the revision editor.
const adminLinkListJS = `(() => {
  const list = document.querySelector('[data-link-list]');
  if (!list) return;
  const bind = (row) => {
    row.querySelector('[data-remove-link]')?.addEventListener('click', () => {
      if (list.children.length > 1) row.remove();
      else row.querySelectorAll('input').forEach((input) => { input.value = ''; });
    });
  };
  list.querySelectorAll('.link-row').forEach(bind);
  document.querySelector('[data-add-link]')?.addEventListener('click', () => {
    if (list.children.length >= 16) return;
    const row = document.createElement('div');
    row.className = 'link-row';
    const title = document.createElement('input');
    title.name = 'link_title';
    title.placeholder = '链接标题';
    const url = document.createElement('input');
    url.name = 'link_url';
    url.type = 'url';
    url.placeholder = 'https://';
    const remove = document.createElement('button');
    remove.className = 'icon-button danger';
    remove.type = 'button';
    remove.dataset.removeLink = '';
    remove.setAttribute('aria-label', '删除链接');
    remove.innerHTML = '<svg class="icon" aria-hidden="true"><use href="#i-delete"></use></svg>';
    row.append(title, url, remove);
    list.appendChild(row);
    bind(row);
  });
})();
`

// adminHomeComposerJS keeps the home banner and card forms in sync with the
// selected target type and handles cover uploads.
const adminHomeComposerJS = `(() => {
  document.querySelectorAll('[data-target-form]').forEach((form) => {
    const select = form.querySelector('[data-target-type]');
    if (!select) return;
    const sync = () => {
      form.querySelectorAll('[data-target]').forEach((field) => {
        const active = field.dataset.target === select.value;
        field.hidden = !active;
        field.querySelectorAll('input,select,textarea').forEach((control) => {
          control.disabled = !active;
          if (!active) control.value = '';
        });
      });
    };
    select.addEventListener('change', sync);
    sync();
  });

  const input = document.getElementById('home-cover-file');
  if (!input) return;
  let target = null;
  document.querySelectorAll('[data-upload-cover]').forEach((button) => {
    button.addEventListener('click', () => {
      target = button.closest('form')?.querySelector('[data-cover-field]');
      if (target) input.click();
    });
  });
  input.addEventListener('change', async () => {
    if (!input.files.length || !target) return;
    const body = new FormData();
    body.append('file', input.files[0]);
    target.setCustomValidity('');
    try {
      const response = await fetch('/admin/blobs', {method: 'POST', body: body, headers: adminCSRFHeaders()});
      if (!response.ok) throw new Error('upload');
      const data = await response.json();
      if (!data.sha256) throw new Error('upload');
      target.value = data.sha256;
    } catch (_) {
      target.setCustomValidity('图片上传失败，请重试');
      target.reportValidity();
    } finally {
      input.value = '';
    }
  });
})();
`

// adminBlogJS covers the create dialog, the cover and inline image uploads and
// the Markdown preview of the writing workspace.
const adminBlogJS = `(() => {
  const dialog = document.querySelector('[data-create-dialog]');
  const trigger = document.querySelector('[data-open-create]');
  if (!dialog || !trigger) return;
  trigger.addEventListener('click', () => {
    dialog.showModal();
    window.setTimeout(() => { dialog.querySelector('input, select, button').focus(); }, 0);
  });
  document.querySelectorAll('[data-close-create]').forEach((button) => {
    button.addEventListener('click', () => { dialog.close(); });
  });
  dialog.addEventListener('close', () => { trigger.focus(); });
})();

(() => {
  const marker = document.getElementById('blog-edit-updated-at');
  const form = document.querySelector('form.blog-editor');
  if (marker && form) form.appendChild(marker);

  const fileInput = document.getElementById('image-file');
  if (!fileInput) return;
  let mode = 'body';
  document.querySelector('[data-upload-image]')?.addEventListener('click', () => { mode = 'body'; fileInput.click(); });
  document.querySelector('[data-upload-cover]')?.addEventListener('click', () => { mode = 'cover'; fileInput.click(); });
  const status = document.getElementById('blog-upload-status');
  fileInput.addEventListener('change', async () => {
    if (!fileInput.files.length) return;
    const body = new FormData();
    body.append('file', fileInput.files[0]);
    if (status) status.textContent = '正在上传图片…';
    try {
      const response = await fetch('/admin/blobs', {method: 'POST', body: body, headers: adminCSRFHeaders()});
      if (!response.ok) throw new Error('upload');
      const data = await response.json();
      if (!data.sha256) throw new Error('upload');
      if (mode === 'cover') {
        document.getElementById('cover-field').value = data.sha256;
        if (status) status.textContent = '封面已更新，保存后生效';
      } else {
        const field = document.getElementById('body-field');
        const insert = '![](/api/blobs/' + data.sha256 + ')';
        const start = field.selectionStart || field.value.length;
        field.value = field.value.slice(0, start) + insert + field.value.slice(field.selectionEnd || start);
        field.focus();
        field.selectionStart = field.selectionEnd = start + insert.length;
        if (status) status.textContent = '图片已插入正文';
      }
    } catch (_) {
      if (status) status.textContent = '图片上传失败，请重试';
    } finally {
      fileInput.value = '';
    }
  });

  const bodyField = document.getElementById('body-field');
  const preview = document.getElementById('body-preview');
  if (!bodyField || !preview) return;
  const renderPreview = () => { preview.textContent = bodyField.value || '正文预览会显示在这里'; };
  bodyField.addEventListener('input', renderPreview);
  renderPreview();
})();
`

// adminHTMXJS is a same-origin subset of HTMX: GET/POST into a selected
// fragment, with history for filters. Full pages still work without it.
const adminHTMXJS = `(() => {
  const csrf = () => document.querySelector('meta[name="csrf-token"]')?.content || '';
  const fragment = (html, select) => {
    const doc = new DOMParser().parseFromString(html, 'text/html');
    return select ? doc.querySelector(select) : doc.body;
  };
  const swap = (target, incoming) => {
    if (!target || !incoming) return;
    target.replaceWith(incoming);
  };
  const request = async (url, options) => {
    const headers = Object.assign({'HX-Request': 'true'}, options.headers || {});
    if (options.method === 'POST' && csrf()) headers['X-CSRF-Token'] = csrf();
    const response = await fetch(url, Object.assign({}, options, {headers, redirect: 'follow'}));
    return response.text();
  };
  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    const getURL = form.getAttribute('hx-get');
    const postURL = event.submitter?.getAttribute('hx-post') || form.getAttribute('hx-post');
    if (!getURL && !postURL) return;
    const targetSel = event.submitter?.getAttribute('hx-target') || form.getAttribute('hx-target');
    const select = event.submitter?.getAttribute('hx-select') || form.getAttribute('hx-select') || targetSel;
    const target = targetSel ? document.querySelector(targetSel) : null;
    if (!target) return;
    event.preventDefault();
    const data = new FormData(form, event.submitter);
    try {
      const html = postURL
        ? await request(postURL, {method: 'POST', body: data})
        : await request(getURL + (getURL.includes('?') ? '&' : '?') + new URLSearchParams(data).toString(), {method: 'GET'});
      swap(target, fragment(html, select));
      if (form.hasAttribute('hx-push-url') || form.getAttribute('hx-push-url') === 'true') {
        const next = postURL || (getURL + (getURL.includes('?') ? '&' : '?') + new URLSearchParams(data).toString());
        history.pushState({}, '', postURL ? location.href : next);
      }
    } catch (_) {
      form.removeAttribute('hx-get');
      form.removeAttribute('hx-post');
      form.submit();
    }
  });
})();
`
