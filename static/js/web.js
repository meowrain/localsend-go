/* LocalSendGo Web 页面交互
 *
 * 组织方式：一个 IIFE，内部按职责分成若干小节（工具 / 列表 / 上传 / 预览 / 设备发送）。
 * 所有用户可控文本都通过 textContent / textarea.value 写入 DOM，绝不拼 innerHTML。
 */
(function () {
  'use strict';

  var CFG = window.__LS__ || {};
  var TOKEN = CFG.token || '';
  var CURRENT_PATH = CFG.path || '';
  var PARENT_PATH = CFG.parent || '';

  // ---------- 工具 ----------

  function $(sel, root) { return (root || document).querySelector(sel); }
  function $all(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  function el(tag, attrs, children) {
    var node = document.createElement(tag);
    if (attrs) {
      Object.keys(attrs).forEach(function (k) {
        if (k === 'class') node.className = attrs[k];
        else if (k === 'text') node.textContent = attrs[k];
        else if (k.slice(0, 2) === 'on') node.addEventListener(k.slice(2), attrs[k]);
        else if (attrs[k] !== null && attrs[k] !== undefined) node.setAttribute(k, attrs[k]);
      });
    }
    (children || []).forEach(function (c) {
      if (c == null) return;
      node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
    });
    return node;
  }

  // fmtTime 把 RFC3339 时间转成本地「YYYY-MM-DD HH:mm」，
  // 与页面首次渲染（服务端格式化）保持一致，避免刷新后格式突变。
  function fmtTime(raw) {
    if (!raw) return '';
    var d = new Date(raw);
    if (isNaN(d.getTime())) return raw;
    function p2(n) { return (n < 10 ? '0' : '') + n; }
    return d.getFullYear() + '-' + p2(d.getMonth() + 1) + '-' + p2(d.getDate()) +
      ' ' + p2(d.getHours()) + ':' + p2(d.getMinutes());
  }

  function humanSize(n) {
    n = Number(n) || 0;
    if (n < 1024) return n + ' B';
    var units = ['KB', 'MB', 'GB', 'TB'];
    var i = -1;
    do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
    return n.toFixed(n >= 10 ? 0 : 1) + ' ' + units[i];
  }

  function joinPath(a, b) {
    if (!a) return b;
    if (!b) return a;
    return a.replace(/\/+$/, '') + '/' + b.replace(/^\/+/, '');
  }

  // 图标与默认停留时长：所有提示统一从这里取，避免各处各写一套
  var TOAST_META = {
    success: { icon: 'fa-circle-check', duration: 3200 },
    error: { icon: 'fa-circle-exclamation', duration: 6000 },
    warn: { icon: 'fa-triangle-exclamation', duration: 4500 },
    info: { icon: 'fa-circle-info', duration: 3200 }
  };

  // toast 支持三种调用方式：
  //   toast('文本')、toast('文本', 'error')、toast({ message, kind, duration, action })
  // action 形如 { label: '撤销', onClick: fn }。
  function toast(message, kind) {
    var opts = {};
    if (typeof message === 'object' && message !== null) {
      opts = message;
    } else {
      opts = { message: message, kind: kind };
    }
    var type = TOAST_META[opts.kind] ? opts.kind : 'info';
    var meta = TOAST_META[type];
    var duration = typeof opts.duration === 'number' ? opts.duration : meta.duration;

    var box = $('#toasts');
    // 同时最多 4 条，超出的从最旧的开始移除，避免刷屏
    var existing = $all('.toast', box);
    if (existing.length >= 4) removeToast(existing[0]);

    var item = el('div', { class: 'toast ' + type, role: 'status' }, [
      el('i', { class: 'fas ' + meta.icon + ' toast-icon' }),
      el('div', { class: 'toast-text', text: opts.message }),
    ]);

    if (opts.action && opts.action.label) {
      // 动作按钮（如「撤销」）：始终可见，样式与关闭按钮区分开
      item.appendChild(el('button', {
        class: 'toast-action',
        text: opts.action.label,
        onclick: function (e) {
          e.stopPropagation();
          opts.action.onClick();
          removeToast(item);
        }
      }));
    }

    item.appendChild(el('button', {
      class: 'toast-close', 'aria-label': '关闭提示', title: '关闭',
      onclick: function () { removeToast(item); }
    }, [el('i', { class: 'fas fa-xmark' })]));

    var bar = el('span', { class: 'toast-bar' });
    bar.style.animationDuration = duration + 'ms';
    item.appendChild(bar);

    box.appendChild(item);

    item._timer = setTimeout(function () { removeToast(item); }, duration);
    // 鼠标悬停暂停倒计时，移出后按剩余时间继续
    item.addEventListener('mouseenter', function () {
      clearTimeout(item._timer);
      item._remaining = null;
    });
    item.addEventListener('mouseleave', function () {
      item._timer = setTimeout(function () { removeToast(item); }, 1800);
    });
    return item;
  }

  function removeToast(item) {
    if (!item || !item.parentNode || item.classList.contains('leaving')) return;
    clearTimeout(item._timer);
    item.classList.add('leaving');
    setTimeout(function () { item.remove(); }, 200);
  }

  function apiError(resp, fallback) {
    return resp.json().then(function (data) {
      return (data && data.message) || fallback;
    }).catch(function () { return fallback; });
  }

  function encodePath(p) {
    return p.split('/').map(encodeURIComponent).join('/');
  }

  // ---------- 模态框 ----------

  var modalBackdrop = $('#modalBackdrop');
  var modalTitle = $('#modalTitle');
  var modalBody = $('#modalBody');
  var modalFoot = $('#modalFoot');
  var modalOnClose = null;

  function openModal(opts) {
    var head = $('.modal-head');
    var oldIcon = $('.modal-icon', head);
    if (oldIcon) oldIcon.remove();
    if (opts.icon) {
      head.insertBefore(el('span', {
        class: 'modal-icon ' + (opts.iconKind || '')
      }, [el('i', { class: 'fas ' + opts.icon })]), modalTitle);
    }

    modalTitle.textContent = opts.title || '';
    modalBody.textContent = '';
    modalFoot.textContent = '';
    if (opts.body) modalBody.appendChild(opts.body);

    (opts.buttons || []).forEach(function (b) {
      var label = el('span', { class: 'btn-label', text: b.label });
      var btn = el('button', {
        class: 'btn ' + (b.kind || ''),
        onclick: function () {
          if (btn.classList.contains('loading')) return;
          if (!b.onClick) { closeModal(); return; }
          var result = b.onClick();
          if (result === false) return;         // 校验失败：保持打开
          if (result === 'keep') return;        // 异步操作接管：成功后再关
          if (result && typeof result.then === 'function') {
            // 异步操作：按钮进入 loading，成功才关闭
            setButtonLoading(btn, true, b.loadingLabel);
            result.then(function () {
              setButtonLoading(btn, false);
              closeModal();
            }, function () {
              setButtonLoading(btn, false);
            });
            return;
          }
          closeModal();
        }
      }, [label, el('span', { class: 'btn-spinner' })]);
      btn._labelEl = label;
      modalFoot.appendChild(btn);
    });

    modalOnClose = opts.onClose || null;
    modalBackdrop.classList.remove('closing', 'hidden');
    if (opts.focus) setTimeout(function () { opts.focus.focus(); }, 60);
  }

  // setButtonLoading 把按钮切到「提交中」状态：文字换文案 + 显示转圈。
  function setButtonLoading(btn, loading, text) {
    if (!btn) return;
    btn.classList.toggle('loading', !!loading);
    btn.disabled = !!loading;
    if (btn._labelEl) {
      if (loading) {
        btn._labelEl.dataset.original = btn._labelEl.dataset.original || btn._labelEl.textContent;
        if (text) btn._labelEl.textContent = text;
      } else if (btn._labelEl.dataset.original) {
        btn._labelEl.textContent = btn._labelEl.dataset.original;
      }
    }
  }

  function closeModal() {
    if (modalBackdrop.classList.contains('hidden')) return;
    modalBackdrop.classList.add('closing');
    // 等退场动画结束再真正隐藏，避免「啪」地消失
    setTimeout(function () {
      modalBackdrop.classList.add('hidden');
      modalBackdrop.classList.remove('closing');
      modalBody.textContent = '';
      modalFoot.textContent = '';
      var icon = $('.modal-icon', $('.modal-head'));
      if (icon) icon.remove();
    }, 170);
    if (modalOnClose) { var f = modalOnClose; modalOnClose = null; f(); }
  }

  $('#modalClose').addEventListener('click', closeModal);
  modalBackdrop.addEventListener('click', function (e) {
    if (e.target === modalBackdrop) closeModal();
  });

  // ---------- 主题 ----------
  //
  // 状态分两段：主题名（THEMES）+ 明暗（light / dark / system）。
  // 解析后的明暗写在 <html data-mode>，主题名写在 <html data-theme>，
  // CSS 只按这 12 种组合取变量，不依赖 prefers-color-scheme 媒体查询。
  // 存储格式：'forest:dark'；兼容老值 'light' / 'dark'（等价于 indigo 主题 + 对应明暗）。

  var THEME_KEY = 'localsendgo-theme';
  var DEFAULT_THEME = 'indigo';
  // 色点用「主题主色 / 主题底色」两个色块拼出来，避免为每套主题再加图标
  var THEMES = [
    { id: 'indigo', name: '靛蓝', hint: '默认', dot: '#4f46e5', dotBg: '#eef2f9' },
    { id: 'forest', name: '森林', hint: '沉稳', dot: '#166534', dotBg: '#e8f0ea' },
    { id: 'ember', name: '暖橙', hint: '温暖', dot: '#b23c0a', dotBg: '#f5ebdf' },
    { id: 'tide', name: '海盐青', hint: '清爽', dot: '#0f766e', dotBg: '#e3eff1' },
    { id: 'rose', name: '玫瑰', hint: '柔和', dot: '#be123c', dotBg: '#f8e7ec' },
    { id: 'synth', name: '赛博紫', hint: '锐利', dot: '#6d28d9', dotBg: '#efe7fd' }
  ];
  var MODES = ['light', 'dark', 'system'];

  function has(list, v) {
    for (var i = 0; i < list.length; i++) if (list[i] === v) return true;
    return false;
  }

  function themeId(v) {
    for (var i = 0; i < THEMES.length; i++) if (THEMES[i].id === v) return v;
    return DEFAULT_THEME;
  }

  function normalizeMode(v) {
    return has(MODES, v) ? v : 'system';
  }

  // state 从 localStorage 读出「主题名 + 明暗」，老值在这里平滑迁移
  function readPref() {
    var raw = null;
    try { raw = localStorage.getItem(THEME_KEY); } catch (e) { raw = null; }
    if (!raw) return { theme: DEFAULT_THEME, mode: 'system' };
    if (raw === 'light' || raw === 'dark') return { theme: DEFAULT_THEME, mode: raw };
    var i = raw.indexOf(':');
    if (i <= 0) return { theme: DEFAULT_THEME, mode: 'system' };
    return { theme: themeId(raw.slice(0, i)), mode: normalizeMode(raw.slice(i + 1)) };
  }

  function savePref(pref) {
    try { localStorage.setItem(THEME_KEY, pref.theme + ':' + pref.mode); } catch (e) { /* 隐私模式忽略 */ }
  }

  function systemDark() {
    return !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
  }

  // resolveMode 把 'system' 解析成实际明暗
  function resolveMode(mode) {
    if (mode === 'system') return systemDark() ? 'dark' : 'light';
    return mode;
  }

  function currentTheme() {
    return resolveMode(readPref().mode);
  }

  // applyTheme 兼容旧签名：传 'light' / 'dark' 只改明暗，传空则重新读存储
  function applyTheme(mode) {
    var pref = readPref();
    if (mode === 'light' || mode === 'dark' || mode === 'system') pref.mode = mode;
    var root = document.documentElement;
    root.setAttribute('data-theme', pref.theme);
    root.setAttribute('data-mode', resolveMode(pref.mode));
    root.setAttribute('data-theme-pref', pref.mode);
  }

  // 主题面板
  var btnTheme = $('#btnTheme');
  var themeMenu = $('#themeMenu');
  var themeList = $('#themeList');
  var themeModeBtns = $all('.theme-modes button', themeMenu);
  var themeMenuOpen = false;
  var themeFocus = -1;
  var themeWrap = themeMenu.parentNode;

  function themeItems() { return $all('.theme-item', themeList); }

  function setThemeFocus(index) {
    var items = themeItems();
    if (!items.length) return;
    themeFocus = (index + items.length) % items.length;
    items[themeFocus].focus();
  }

  function syncThemeUI() {
    var pref = readPref();
    themeModeBtns.forEach(function (b) {
      b.setAttribute('aria-pressed', b.dataset.mode === pref.mode ? 'true' : 'false');
    });
    themeItems().forEach(function (it) {
      var on = it.dataset.theme === pref.theme;
      it.setAttribute('aria-selected', on ? 'true' : 'false');
      it.tabIndex = on ? 0 : -1;
      var check = $('.theme-check', it);
      if (check) check.style.visibility = on ? 'visible' : 'hidden';
    });
    btnTheme.setAttribute('title', '外观主题');
  }

  function chooseTheme(id) {
    var pref = readPref();
    pref.theme = themeId(id);
    savePref(pref);
    applyTheme();
    syncThemeUI();
  }

  function chooseMode(mode) {
    var pref = readPref();
    pref.mode = normalizeMode(mode);
    savePref(pref);
    applyTheme();
    syncThemeUI();
  }

  function buildThemeMenu() {
    THEMES.forEach(function (t) {
      var check = el('i', { class: 'fas fa-check theme-check' });
      var dot = el('span', { class: 'theme-dot' });
      dot.style.background = 'linear-gradient(135deg, ' + t.dot + ' 0 50%, ' + t.dotBg + ' 50% 100%)';
      var item = el('li', {}, [
        el('button', {
          class: 'theme-item',
          type: 'button',
          role: 'option',
          'data-theme': t.id,
          'aria-selected': 'false',
          title: '切换到「' + t.name + '」主题',
          onclick: function () { chooseTheme(t.id); }
        }, [
          dot,
          el('span', { class: 'theme-name', text: t.name }),
          el('span', { class: 'theme-hint', text: t.hint }),
          check
        ])
      ]);
      themeList.appendChild(item);
    });

    themeModeBtns.forEach(function (b) {
      b.addEventListener('click', function () { chooseMode(b.dataset.mode); });
    });

    // 列表内键盘：↑↓ 移动，Home/End 两端，回车/空格直接选中
    themeList.addEventListener('keydown', function (e) {
      var items = themeItems();
      var idx = items.indexOf(document.activeElement);
      if (e.key === 'ArrowDown') { e.preventDefault(); setThemeFocus(idx < 0 ? 0 : idx + 1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setThemeFocus(idx < 0 ? items.length - 1 : idx - 1); }
      else if (e.key === 'Home') { e.preventDefault(); setThemeFocus(0); }
      else if (e.key === 'End') { e.preventDefault(); setThemeFocus(items.length - 1); }
      else if (e.key === 'Enter' || e.key === ' ') {
        if (idx >= 0) { e.preventDefault(); chooseTheme(items[idx].dataset.theme); }
      }
    });
  }

  function openThemeMenu() {
    if (themeMenuOpen) return;
    themeMenuOpen = true;
    themeMenu.classList.remove('hidden');
    btnTheme.setAttribute('aria-expanded', 'true');
    syncThemeUI();
    // 把焦点放到当前主题上，键盘用户可以直接上下选择
    var pref = readPref();
    var items = themeItems();
    for (var i = 0; i < items.length; i++) {
      if (items[i].dataset.theme === pref.theme) { setThemeFocus(i); break; }
    }
    if (themeFocus < 0 && items.length) setThemeFocus(0);
  }

  function closeThemeMenu(refocus) {
    if (!themeMenuOpen) return;
    themeMenuOpen = false;
    themeMenu.classList.add('hidden');
    btnTheme.setAttribute('aria-expanded', 'false');
    themeFocus = -1;
    if (refocus) btnTheme.focus();
  }

  btnTheme.addEventListener('click', function (e) {
    e.stopPropagation();
    if (themeMenuOpen) closeThemeMenu(true); else openThemeMenu();
  });

  // 点击面板外部关闭
  document.addEventListener('click', function (e) {
    if (!themeMenuOpen) return;
    if (themeWrap.contains(e.target)) return;
    closeThemeMenu(false);
  });

  // Esc 关闭；面板打开时上下键交给面板自己处理
  document.addEventListener('keydown', function (e) {
    if (!themeMenuOpen) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      closeThemeMenu(true);
    }
  }, true);

  // 跟随系统：系统外观变化时实时同步（只在 mode=system 时生效）
  if (window.matchMedia) {
    var mqDark = window.matchMedia('(prefers-color-scheme: dark)');
    var onSchemeChange = function () {
      if (readPref().mode === 'system') applyTheme();
    };
    if (mqDark.addEventListener) mqDark.addEventListener('change', onSchemeChange);
    else if (mqDark.addListener) mqDark.addListener(onSchemeChange);
  }

  buildThemeMenu();
  applyTheme();
  syncThemeUI();

  // ---------- 列表：搜索 / 排序 / 选择 ----------

  var fileList = $('#fileList');
  var searchInput = $('#searchInput');
  var viewSelect = $('#viewSelect');
  var sortSelect = $('#sortSelect');
  var selectionInfo = $('#selectionInfo');
  var btnDeleteSelected = $('#btnDeleteSelected');
  var btnDownloadSelected = $('#btnDownloadSelected');

  function rows() {
    return $all('.file-row', fileList).filter(function (r) { return r.dataset.action !== 'up'; });
  }

  function selected() {
    return rows().filter(function (r) {
      var cb = $('.check', r);
      return cb && cb.checked;
    });
  }

  function updateSelection() {
    var sel = selected();
    var n = sel.length;
    selectionInfo.textContent = n ? '已选 ' + n + ' 项' : '';
    btnDeleteSelected.classList.toggle('hidden', n === 0);
    btnDownloadSelected.classList.toggle('hidden', n === 0);
    rows().forEach(function (r) {
      var cb = $('.check', r);
      r.classList.toggle('selected', !!(cb && cb.checked));
    });
  }

  // 行级动画：删除前淡出、插入时淡入、操作成功后脉冲高亮。
  // 都在 DOM 上做，不依赖重新渲染整张列表。
  function animateRowLeave(row) {
    return new Promise(function (resolve) {
      if (!row || !row.parentNode) { resolve(); return; }
      row.classList.add('row-leave');
      setTimeout(function () {
        if (row.parentNode) row.remove();
        resolve();
      }, 240);
    });
  }

  function flashRow(row) {
    if (!row) return;
    row.classList.remove('flash');
    // 强制重排，让动画可以重复触发
    void row.offsetWidth;
    row.classList.add('flash');
    setTimeout(function () { row.classList.remove('flash'); }, 950);
  }

  function applySort() {
    var mode = sortSelect.value;
    var items = rows();
    items.sort(function (a, b) {
      var ad = a.dataset.dir === '1', bd = b.dataset.dir === '1';
      if (ad !== bd) return ad ? -1 : 1; // 目录恒在前
      var an = a.dataset.name.toLowerCase(), bn = b.dataset.name.toLowerCase();
      switch (mode) {
        case 'name-desc': return bn.localeCompare(an, 'zh-CN');
        case 'time-desc': return (b.dataset.time || '').localeCompare(a.dataset.time || '');
        case 'time-asc': return (a.dataset.time || '').localeCompare(b.dataset.time || '');
        case 'size-desc': return Number(b.dataset.size) - Number(a.dataset.size);
        case 'size-asc': return Number(a.dataset.size) - Number(b.dataset.size);
        default: return an.localeCompare(bn, 'zh-CN');
      }
    });
    items.forEach(function (r) { fileList.appendChild(r); });
  }

  function applyFilter() {
    var q = searchInput.value.trim().toLowerCase();
    rows().forEach(function (r) {
      var hit = !q || r.dataset.name.toLowerCase().indexOf(q) !== -1;
      r.classList.toggle('hidden', !hit);
    });
  }

  searchInput.addEventListener('input', applyFilter);
  sortSelect.addEventListener('change', function () {
    applySort();
    localStorage.setItem('localsendgo-sort', sortSelect.value);
  });
  sortSelect.value = localStorage.getItem('localsendgo-sort') || 'name-asc';
  if (rows().length) { applySort(); }

  // ---------- 视图 ----------

  var VIEW_KEY = 'localsendgo-view';
  var VIEW_SIZES = {
    xlarge: 320,
    large: 224,
    medium: 168,
    small: 80,
    list: 96,
    details: 96,
    tiles: 96,
    content: 96
  };

  function applyView(mode) {
    if (!VIEW_SIZES[mode]) mode = 'details';
    fileList.dataset.view = mode;
    $all('.file-row', fileList).forEach(function (row) {
      if (row.dataset.dir === '1' || (row.dataset.mime || '').indexOf('image/') !== 0) return;
      var img = $('.file-thumb', row);
      var url = thumbUrl(row);
      if (img) img.src = url;
      else applyThumb(row);
    });
  }

  viewSelect.addEventListener('change', function () {
    applyView(viewSelect.value);
    localStorage.setItem(VIEW_KEY, viewSelect.value);
  });

  viewSelect.value = localStorage.getItem(VIEW_KEY) || 'details';

  // ---------- 缩略图 ----------
  //
  // 服务端只解码 PNG/JPEG/GIF 并缩到请求的长边；其余图片类型后端会 302 到
  // 预览接口，由浏览器自己缩小原图。任何失败都静默回退到类型图标。
  //
  // v= 参数让图片换版本时 URL 跟着变：缩略图带 1 天 private 缓存，
  // 不换 URL 的话重命名/覆盖同名文件后浏览器还会拿旧图。

  function thumbUrl(row) {
    var q = '?path=' + encodeURIComponent(row.dataset.path) +
      '&size=' + encodeURIComponent(VIEW_SIZES[fileList.dataset.view] || 96) +
      '&v=' + encodeURIComponent((row.dataset.time || '') + '|' + (row.dataset.size || ''));
    return '/api/web/thumb' + q;
  }

  function applyThumb(row) {
    if (row.dataset.dir === '1') return;
    if ((row.dataset.mime || '').indexOf('image/') !== 0) return;
    var iconBox = $('.file-icon', row);
    if (!iconBox || iconBox.querySelector('.file-thumb')) return;

    var img = el('img', {
      class: 'file-thumb',
      alt: '',
      loading: 'lazy',
      decoding: 'async'
    });
    img.addEventListener('load', function () {
      // 图标在缩略图盖上来之前一直保留，避免加载过程中闪成空白方块
      iconBox.classList.add('has-thumb');
    });
    img.addEventListener('error', function () {
      iconBox.classList.remove('has-thumb');
      img.remove();
    });
    img.src = thumbUrl(row);
    iconBox.appendChild(img);
  }

  $all('.file-row', fileList).forEach(applyThumb);
  applyView(viewSelect.value);

  // ---------- 行操作 ----------

  function rowAction(row) {
    var path = row.dataset.path;
    var isDir = row.dataset.dir === '1';
    if (isDir) location.href = '/' + encodePath(path) + '/';
    else if (isPreviewablePath(path)) previewFile(path);
    else downloadFile(path);
  }

  function downloadFile(path) {
    var a = el('a', { href: '/uploads/' + encodePath(path), download: '' });
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  function downloadPaths(paths) {
    if (paths.length === 1) {
      // 单文件直接下；单个目录也交给后端打 zip
      var a = el('a', { href: '/api/web/download?path=' + encodeURIComponent(paths[0]) });
      document.body.appendChild(a);
      a.click();
      a.remove();
      return;
    }
    var q = paths.map(function (p) { return 'paths=' + encodeURIComponent(p); }).join('&');
    var b = el('a', { href: '/api/web/download?' + q });
    document.body.appendChild(b);
    b.click();
    b.remove();
  }


  // ---------- 图片查看器 ----------
  //
  // 缩放的基准是「图片原始像素」：scale = 1 表示 1:1，
  // 「适应窗口」会在每次打开/窗口变化时重新计算 fitScale。
  // 平移用 translate + scale，原点在图片中心。

  var viewerEl = $('#imageViewer');
  var viewerStage = $('#viewerStage');
  var viewerImg = $('#viewerImg');
  var viewerLoading = $('#viewerLoading');
  var viewerError = $('#viewerError');
  var viewerName = $('#viewerName');
  var viewerMeta = $('#viewerMeta');
  var viewerCount = $('#viewerCount');

  var viewer = {
    open: false,
    list: [],       // 同一目录下的图片路径
    index: -1,
    scale: 1,
    fitScale: 1,
    x: 0,
    y: 0,
    rotate: 0,
    dragging: false,
    dragStart: null,
    pressStart: null,
    slid: false,
    lastTap: 0,
    loadToken: 0
  };

  var MIN_SCALE = 0.02;
  var MAX_SCALE = 32;

  function isImagePath(p) {
    return /\.(png|jpe?g|gif|webp|bmp|svg|ico|avif|heic|tiff?)$/i.test(p || '');
  }

  // 音视频/PDF 也能在线预览，点击文件名时直接打开预览弹窗
  function isPreviewablePath(p) {
    return isImagePath(p) ||
      /\.(mp4|webm|ogv|mov|m4v|mp3|wav|flac|aac|m4a|opus|pdf)$/i.test(p || '');
  }

  // imagePathsInCurrentDir 收集当前列表里的图片，用于上一张/下一张
  function imagePathsInCurrentDir() {
    return rows()
      .filter(function (r) {
        return r.dataset.dir !== '1' && isImagePath(r.dataset.name);
      })
      .map(function (r) { return r.dataset.path; });
  }

  function viewerUrl(path) {
    return '/api/web/preview?path=' + encodeURIComponent(path);
  }

  function clampScale(v) {
    return Math.min(MAX_SCALE, Math.max(MIN_SCALE, v));
  }

  function computeFitScale() {
    var nw = viewerImg.naturalWidth || 1;
    var nh = viewerImg.naturalHeight || 1;
    var stageW = viewerStage.clientWidth - 24;
    var stageH = viewerStage.clientHeight - 24;
    if (stageW <= 0 || stageH <= 0) return 1;
    // 旋转 90/270 度时宽高互换
    var portrait = Math.abs(viewer.rotate % 180) === 90;
    var w = portrait ? nh : nw;
    var h = portrait ? nw : nh;
    return Math.min(stageW / w, stageH / h, 1);
  }

  // 把图片当前可见的包围盒限制在舞台内：图片小于舞台时居中，
  // 大于舞台时允许拖到边缘但不能整个拖出视野（常见看图工具的边界吸附）。
  function clampPan() {
    var nw = viewerImg.naturalWidth;
    var nh = viewerImg.naturalHeight;
    if (!nw || !nh) return;
    var portrait = Math.abs(viewer.rotate % 180) === 90;
    var dispW = (portrait ? nh : nw) * viewer.scale;
    var dispH = (portrait ? nw : nh) * viewer.scale;
    var stageW = viewerStage.clientWidth;
    var stageH = viewerStage.clientHeight;
    var maxX = Math.max(0, (dispW - stageW) / 2);
    var maxY = Math.max(0, (dispH - stageH) / 2);
    viewer.x = Math.min(maxX, Math.max(-maxX, viewer.x));
    viewer.y = Math.min(maxY, Math.max(-maxY, viewer.y));
  }

  function applyTransform(smooth) {
    clampPan();
    viewerImg.classList.toggle('smooth', !!smooth);
    // -50% 先把图片中心移到舞台中心，再叠加用户平移/旋转/缩放
    viewerImg.style.transform =
      'translate(-50%,-50%) translate(' + viewer.x + 'px,' + viewer.y + 'px) rotate(' +
      viewer.rotate + 'deg) scale(' + viewer.scale + ')';
    viewerStage.classList.toggle('zoomed', viewer.scale > viewer.fitScale * 1.01);
    updateViewerMeta();
  }

  function updateViewerMeta() {
    var nw = viewerImg.naturalWidth;
    var nh = viewerImg.naturalHeight;
    var parts = [];
    if (nw && nh) parts.push(nw + ' × ' + nh);
    parts.push(Math.round(viewer.scale * 100) + '%');
    if (viewer.rotate) parts.push(viewer.rotate + '°');
    viewerMeta.textContent = parts.join(' · ');
  }

  function zoomTo(scale, anchorX, anchorY, smooth) {
    var prev = viewer.scale;
    var next = clampScale(scale);
    if (next === prev) { applyTransform(smooth); return; }
    if (anchorX === undefined) {
      // 以舞台中心为锚点：保持中心不动
      anchorX = viewerStage.clientWidth / 2;
      anchorY = viewerStage.clientHeight / 2;
    }
    // 锚点相对图片中心的位置
    var cx = viewerStage.clientWidth / 2;
    var cy = viewerStage.clientHeight / 2;
    var dx = anchorX - cx - viewer.x;
    var dy = anchorY - cy - viewer.y;
    var ratio = next / prev;
    viewer.x -= dx * (ratio - 1);
    viewer.y -= dy * (ratio - 1);
    viewer.scale = next;
    applyTransform(smooth);
  }

  function zoomBy(factor, anchorX, anchorY) {
    zoomTo(viewer.scale * factor, anchorX, anchorY, false);
  }

  function fitToWindow() {
    viewer.fitScale = computeFitScale();
    viewer.scale = viewer.fitScale;
    viewer.rotate = 0;
    viewer.x = 0;
    viewer.y = 0;
    applyTransform(true);
  }

  function actualSize() {
    viewer.scale = 1;
    viewer.x = 0;
    viewer.y = 0;
    applyTransform(true);
  }

  function rotateBy(deg) {
    var next = viewer.rotate + deg;
    // 旋转后按新的包围盒重新计算「适应窗口」，否则宽图旋转完会超出屏幕
    viewer.rotate = next;
    if (viewer.scale <= viewer.fitScale * 1.01) {
      viewer.fitScale = computeFitScale();
      viewer.scale = viewer.fitScale;
      viewer.x = 0;
      viewer.y = 0;
    }
    applyTransform(true);
  }

  function showViewerImage(path, opts) {
    opts = opts || {};
    viewer.name = path.split('/').pop();
    viewerName.textContent = viewer.name;
    viewerMeta.textContent = '';
    viewerCount.textContent = (viewer.index + 1) + ' / ' + viewer.list.length;
    viewerLoading.classList.remove('hidden');
    viewerError.classList.add('hidden');
    viewerImg.classList.remove('loaded');
    viewerImg.style.visibility = 'hidden';

    var token = ++viewer.loadToken;
    var url = viewerUrl(path);
    var img = new Image();
    img.onload = function () {
      if (token !== viewer.loadToken) return; // 已切到别的图片
      viewerImg.src = url;
      viewerImg.alt = viewer.name;
      viewerImg.style.visibility = '';
      viewerLoading.classList.add('hidden');
      // 等一帧让 naturalWidth 可用
      requestAnimationFrame(function () {
        if (token !== viewer.loadToken) return;
        fitToWindow();
      });
    };
    img.onerror = function () {
      if (token !== viewer.loadToken) return;
      viewerLoading.classList.add('hidden');
      viewerImg.style.visibility = 'hidden';
      viewerError.textContent = '图片加载失败：' + viewer.name;
      viewerError.classList.remove('hidden');
    };
    img.src = url;
    // 预加载相邻图片，切换时更顺滑
    preloadNeighbors();
  }

  function preloadNeighbors() {
    [-1, 1].forEach(function (d) {
      var p = viewer.list[viewer.index + d];
      if (p) { var im = new Image(); im.src = viewerUrl(p); }
    });
  }

  function openImageViewer(path) {
    viewer.list = imagePathsInCurrentDir();
    if (!viewer.list.length) viewer.list = [path];
    viewer.index = viewer.list.indexOf(path);
    if (viewer.index < 0) { viewer.list.push(path); viewer.index = viewer.list.length - 1; }

    viewer.x = 0; viewer.y = 0; viewer.rotate = 0; viewer.scale = 1;
    viewer.open = true;
    // 单张图片时禁用上一张/下一张
    var many = viewer.list.length > 1;
    viewerEl.querySelectorAll('[data-v="prev"],[data-v="next"]').forEach(function (b) {
      b.disabled = !many;
    });
    viewerEl.classList.remove('hidden');
    document.body.style.overflow = 'hidden';
    showViewerImage(path);
    viewerEl.querySelector('[data-v="close"]').focus();
  }

  function closeImageViewer() {
    viewer.open = false;
    viewer.loadToken++; // 让在途的 onload 失效
    viewerEl.classList.add('hidden');
    document.body.style.overflow = '';
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(function () {});
    }
    viewerImg.removeAttribute('src');
  }

  function stepImage(delta) {
    if (!viewer.list.length) return;
    viewer.index = (viewer.index + delta + viewer.list.length) % viewer.list.length;
    viewer.x = 0; viewer.y = 0; viewer.rotate = 0;
    showViewerImage(viewer.list[viewer.index]);
  }

  function toggleViewerFullscreen() {
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(function () {});
    } else if (viewerEl.requestFullscreen) {
      viewerEl.requestFullscreen().catch(function () {});
    }
  }

  function downloadCurrentImage() {
    if (viewer.index >= 0 && viewer.list[viewer.index]) downloadFile(viewer.list[viewer.index]);
  }

  // 工具栏点击
  viewerEl.addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-v]');
    if (!btn) return;
    var act = btn.dataset.v;
    if (act === 'close') closeImageViewer();
    else if (act === 'zoom-in') zoomBy(1.25);
    else if (act === 'zoom-out') zoomBy(1 / 1.25);
    else if (act === 'zoom-fit') fitToWindow();
    else if (act === 'zoom-actual') actualSize();
    else if (act === 'rotate-left') rotateBy(-90);
    else if (act === 'rotate-right') rotateBy(90);
    else if (act === 'fullscreen') toggleViewerFullscreen();
    else if (act === 'download') downloadCurrentImage();
    else if (act === 'prev') stepImage(-1);
    else if (act === 'next') stepImage(1);
  });

  // 滚轮缩放：以鼠标位置为锚点
  viewerStage.addEventListener('wheel', function (e) {
    if (!viewer.open) return;
    e.preventDefault();
    var rect = viewerStage.getBoundingClientRect();
    var factor = Math.exp(-e.deltaY * 0.0018);
    zoomBy(factor, e.clientX - rect.left, e.clientY - rect.top);
  }, { passive: false });

  // 双击：适应窗口 <-> 原始大小（已放大时回到适应）
  viewerStage.addEventListener('dblclick', function (e) {
    e.preventDefault();
    if (viewer.scale > viewer.fitScale * 1.01) {
      fitToWindow();
    } else {
      var rect = viewerStage.getBoundingClientRect();
      zoomTo(Math.max(1, viewer.fitScale * 2.5), e.clientX - rect.left, e.clientY - rect.top, true);
    }
  });

  // 指针拖拽平移 + 双指捏合缩放
  var pointers = new Map();
  var pinchStart = null;

  viewerStage.addEventListener('pointerdown', function (e) {
    if (!viewer.open) return;
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    viewerStage.setPointerCapture(e.pointerId);
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });

    if (pointers.size === 1) {
      viewer.dragging = true;
      viewer.dragStart = { x: e.clientX - viewer.x, y: e.clientY - viewer.y };
      // 记录按下位置，用来区分「点击」与「拖动」，以及触摸左右滑动切换
      viewer.pressStart = { x: e.clientX, y: e.clientY, t: Date.now() };
      viewer.slid = false;
      viewerStage.classList.add('dragging');
      viewerImg.classList.remove('smooth');
    } else if (pointers.size === 2) {
      // 进入捏合：记录初始距离与中点
      var pts = Array.from(pointers.values());
      var dist = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
      var rect = viewerStage.getBoundingClientRect();
      pinchStart = {
        dist: dist,
        scale: viewer.scale,
        cx: (pts[0].x + pts[1].x) / 2 - rect.left,
        cy: (pts[0].y + pts[1].y) / 2 - rect.top
      };
      viewer.dragging = false;
      viewerStage.classList.remove('dragging');
    }
  });

  viewerStage.addEventListener('pointermove', function (e) {
    if (!viewer.open || !pointers.has(e.pointerId)) return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });

    if (pointers.size === 2 && pinchStart) {
      var pts = Array.from(pointers.values());
      var dist = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
      if (pinchStart.dist > 0) {
        zoomTo(pinchStart.scale * (dist / pinchStart.dist), pinchStart.cx, pinchStart.cy, false);
      }
      return;
    }
    if (!viewer.dragging) return;
    viewer.x = e.clientX - viewer.dragStart.x;
    viewer.y = e.clientY - viewer.dragStart.y;
    if (viewer.pressStart) {
      var dx = e.clientX - viewer.pressStart.x;
      var dy = e.clientY - viewer.pressStart.y;
      if (Math.abs(dx) > 6 || Math.abs(dy) > 6) viewer.slid = true;
    }
    applyTransform(false);
  });

  function endPointer(e) {
    pointers.delete(e.pointerId);
    if (pointers.size < 2) pinchStart = null;
    if (pointers.size !== 0) return;

    viewer.dragging = false;
    viewerStage.classList.remove('dragging');
    // 双指缩放结束后如果缩得比适应窗口还小，就贴回适应窗口
    if (viewer.scale < viewer.fitScale * 0.99) fitToWindow();

    var start = viewer.pressStart;
    viewer.pressStart = null;
    if (!start) return;
    var dx = e.clientX - start.x;
    var dy = e.clientY - start.y;
    var atFit = viewer.scale <= viewer.fitScale * 1.01;

    // 触摸/鼠标左右滑动：在适应窗口状态下切换上一张/下一张
    if (atFit && Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) {
      stepImage(dx < 0 ? 1 : -1);
      return;
    }
    // 单击图片之外的空白区域关闭（与常见看图工具一致）
    if (!viewer.slid && start.t && Date.now() - start.t < 400) {
      var rect = viewerImg.getBoundingClientRect();
      var outside = e.clientX < rect.left || e.clientX > rect.right ||
                    e.clientY < rect.top || e.clientY > rect.bottom;
      if (outside) closeImageViewer();
    }
  }
  viewerStage.addEventListener('pointerup', endPointer);
  viewerStage.addEventListener('pointercancel', endPointer);

  // 窗口尺寸变化时重新计算适应比例（保持在适应状态时跟着变）
  window.addEventListener('resize', function () {
    if (!viewer.open || !viewerImg.naturalWidth) return;
    var wasFit = viewer.scale <= viewer.fitScale * 1.01;
    viewer.fitScale = computeFitScale();
    if (wasFit) {
      viewer.scale = viewer.fitScale;
      viewer.x = 0;
      viewer.y = 0;
      applyTransform(false);
    }
  });

  // 查看器内的键盘操作优先于页面快捷键
  document.addEventListener('keydown', function (e) {
    if (!viewer.open) return;
    var tag = (e.target.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea') return;
    var handled = true;
    switch (e.key) {
      case 'Escape': closeImageViewer(); break;
      case 'ArrowLeft': stepImage(-1); break;
      case 'ArrowRight': stepImage(1); break;
      case '+': case '=': zoomBy(1.25); break;
      case '-': case '_': zoomBy(1 / 1.25); break;
      case '0': fitToWindow(); break;
      case '1': actualSize(); break;
      case 'r': rotateBy(90); break;
      case 'R': rotateBy(-90); break;
      case 'f': case 'F': toggleViewerFullscreen(); break;
      case 'd': case 'D': downloadCurrentImage(); break;
      default: handled = false;
    }
    if (handled) {
      e.preventDefault();
      // 阻止页面级快捷键（如 Esc 关闭模态框、/ 聚焦搜索）同时生效
      e.stopImmediatePropagation();
    }
  }, true);

  function previewFile(path) {
    var url = '/api/web/preview?path=' + encodeURIComponent(path);
    var name = path.split('/').pop();
    var ext = (name.split('.').pop() || '').toLowerCase();
    var body;

    if (/^(png|jpe?g|gif|webp|bmp|svg|ico|avif|heic|tiff?)$/.test(ext)) {
      openImageViewer(path);
      return;
    } else if (/^(mp4|webm|ogg|mov|m4v)$/.test(ext)) {
      body = el('video', { class: 'preview-media', src: url, controls: '', preload: 'metadata' });
    } else if (/^(mp3|wav|flac|aac|m4a|opus)$/.test(ext)) {
      body = el('audio', { src: url, controls: '', style: 'width:100%' });
    } else if (ext === 'pdf') {
      body = el('iframe', { class: 'preview-frame', src: url, title: name });
    } else if (/^(txt|md|log|csv|json|ya?ml|toml|xml|html?|css|js|go|py|rs|c|cpp|h|java|ts|tsx|sh|ini|conf)$/.test(ext)) {
      body = el('pre', { class: 'preview-text', text: '加载中…' });
      fetch(url, { credentials: 'same-origin' }).then(function (r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.text();
      }).then(function (text) {
        // 限制渲染长度，避免超大日志把页面卡死
        body.textContent = text.length > 200000 ? text.slice(0, 200000) + '\n\n…（已截断）' : text;
      }).catch(function (e) {
        body.textContent = '无法预览：' + e.message;
      });
    } else {
      body = el('div', { class: 'empty', text: '该类型不支持在线预览，请直接下载。' });
    }

    openModal({
      title: name,
      body: body,
      buttons: [
        { label: '下载', kind: 'primary', onClick: function () { downloadFile(path); } },
        { label: '关闭' }
      ]
    });
  }

  function renameEntry(path, oldName) {
    var input = el('input', { value: oldName, style: 'width:100%' });
    var row = rowByPath(path);
    var isDir = row && row.dataset.dir === '1';
    var targetDir = path.indexOf('/') === -1 ? '' : path.slice(0, path.lastIndexOf('/'));

    openModal({
      title: isDir ? '重命名文件夹' : '重命名文件',
      icon: 'fa-pen',
      body: el('div', {}, [
        el('p', { text: '原名称：' + oldName, style: 'margin-top:0;color:var(--text-muted);font-size:.85rem' }),
        input
      ]),
      focus: input,
      buttons: [
        { label: '取消' },
        {
          label: '保存', kind: 'primary', loadingLabel: '保存中…',
          onClick: function () {
            var name = input.value.trim();
            if (!name) { toast({ message: '请输入新名称', kind: 'warn' }); input.focus(); return false; }
            if (name === oldName) { closeModal(); return 'keep'; }
            var to = joinPath(targetDir, name);
            return fetch('/api/web/rename', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json', 'X-Web-Token': TOKEN },
              body: JSON.stringify({ from: path, to: to })
            }).then(function (r) {
              if (!r.ok) return apiError(r, '重命名失败').then(function (m) { throw new Error(m); });
              return r.json();
            }).then(function () {
              toast({ message: '已重命名为「' + name + '」', kind: 'success', duration: 4500, action: {
                label: '撤销', onClick: function () { undoRename(path, to); }
              } });
              refreshList().then(function () { flashRow(rowByPath(to)); });
            }).catch(function (e) {
              toast({ message: e.message || '重命名失败', kind: 'error' });
              throw e;
            });
          }
        }
      ]
    });

    input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') { e.preventDefault(); $all('button', modalFoot)[1].click(); }
      if (e.key === 'Escape') { e.preventDefault(); closeModal(); }
    });
  }

  // undoRename 把文件改回原名（撤销上一次重命名）。
  function undoRename(from, to) {
    fetch('/api/web/rename', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Web-Token': TOKEN },
      body: JSON.stringify({ from: to, to: from })
    }).then(function (r) {
      if (!r.ok) return apiError(r, '撤销失败').then(function (m) { throw new Error(m); });
      toast({ message: '已撤销重命名', kind: 'info' });
      refreshList().then(function () { flashRow(rowByPath(from)); });
    }).catch(function (e) {
      toast({ message: e.message || '撤销失败', kind: 'error' });
    });
  }

  function deletePaths(paths) {
    paths = (paths || []).filter(Boolean);
    if (!paths.length) return;

    var hasDir = paths.some(function (p) {
      var row = rowByPath(p);
      return row && row.dataset.dir === '1';
    });
    var onlyOne = paths.length === 1;
    var firstName = paths[0].split('/').pop();

    var list = el('ul', { class: 'confirm-list' });
    paths.forEach(function (p) {
      var row = rowByPath(p);
      var isDir = row && row.dataset.dir === '1';
      list.appendChild(el('li', {}, [
        el('i', { class: 'fas ' + (isDir ? 'fa-folder' : 'fa-file') }),
        el('span', { text: p })
      ]));
    });

    var body = el('div', {}, [
      el('p', {
        style: 'margin-top:0',
        text: onlyOne
          ? '确定要删除「' + firstName + '」吗？'
          : '确定要删除以下 ' + paths.length + ' 项吗？'
      }),
      list,
      el('p', {
        style: 'margin:10px 0 0;font-size:.8rem;color:var(--danger)',
        text: hasDir ? '文件夹及其中的全部内容都会被删除，且无法恢复。' : '删除后无法恢复。'
      })
    ]);

    openModal({
      title: hasDir ? '删除文件夹' : '删除文件',
      icon: 'fa-triangle-exclamation',
      iconKind: 'danger',
      body: body,
      buttons: [
        { label: '取消' },
        {
          label: onlyOne ? '删除' : '删除 ' + paths.length + ' 项',
          kind: 'danger',
          loadingLabel: '删除中…',
          onClick: function () {
            var rows = paths.map(rowByPath).filter(Boolean);
            return fetch('/api/web/delete', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json', 'X-Web-Token': TOKEN },
              body: JSON.stringify({ paths: paths, recursive: true })
            }).then(function (r) {
              if (!r.ok) return apiError(r, '删除失败').then(function (m) { throw new Error(m); });
              return r.json();
            }).then(function (data) {
              var done = (data && data.deleted) || [];
              // 先播放行淡出，再从列表里移除；结束后局部刷新与服务器对齐
              return Promise.all(rows.map(animateRowLeave)).then(function () {
                toast({
                  message: onlyOne
                    ? '已删除「' + firstName + '」'
                    : '已删除 ' + done.length + ' 项',
                  kind: 'success'
                });
                refreshList();
              });
            }).catch(function (e) {
              toast({ message: e.message || '删除失败', kind: 'error' });
              throw e; // 让按钮退出 loading，弹窗保持打开
            });
          }
        }
      ]
    });
  }

  // rowByPath 按相对路径找当前列表里的行。
  function rowByPath(path) {
    return rows().filter(function (r) { return r.dataset.path === path; })[0] || null;
  }

  function newFolder() {
    var input = el('input', { placeholder: '文件夹名称（可用 / 分隔多级）', style: 'width:100%' });
    openModal({
      title: '新建文件夹',
      icon: 'fa-folder-plus',
      body: input,
      focus: input,
      buttons: [
        { label: '取消' },
        {
          label: '创建', kind: 'primary', loadingLabel: '创建中…',
          onClick: function () {
            var name = input.value.trim();
            if (!name) { toast({ message: '请输入文件夹名称', kind: 'warn' }); input.focus(); return false; }
            var p = joinPath(CURRENT_PATH, name);
            return fetch('/api/web/mkdir', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json', 'X-Web-Token': TOKEN },
              body: JSON.stringify({ path: p })
            }).then(function (r) {
              if (!r.ok) return apiError(r, '创建失败').then(function (m) { throw new Error(m); });
              return r.json();
            }).then(function () {
              toast({ message: '已创建文件夹「' + name + '」', kind: 'success' });
              return refreshList().then(function () { flashRow(rowByPath(p)); });
            }).catch(function (e) {
              toast({ message: e.message || '创建失败', kind: 'error' });
              throw e;
            });
          }
        }
      ]
    });
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') { e.preventDefault(); $all('button', modalFoot)[1].click(); }
      if (e.key === 'Escape') { e.preventDefault(); closeModal(); }
    });
  }

  function copyLink(path) {
    var url = location.origin + '/uploads/' + encodePath(path);
    var done = function () {
      toast({
        kind: 'success',
        message: CFG.pinEnabled
          ? '链接已复制（本机启用了 PIN，对方需要登录后才能下载）'
          : '链接已复制'
      });
    };
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(url).then(done).catch(function () { fallbackCopy(url, done); });
    } else {
      fallbackCopy(url, done);
    }
  }

  function fallbackCopy(text, done) {
    var ta = el('textarea', { value: text, style: 'position:fixed;opacity:0' });
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); done(); } catch (e) { toast({ message: '复制失败，请手动复制：' + text, kind: 'error' }); }
    ta.remove();
  }

  function shareOrSend(path) {
    // 移动端优先调起系统分享，桌面端直接进入「发送到设备」
    if (navigator.share) {
      var url = location.origin + '/uploads/' + encodePath(path);
      navigator.share({ title: path.split('/').pop(), url: url }).catch(function () {
        openSendPanel([path]);
      });
      return;
    }
    openSendPanel([path]);
  }

  fileList.addEventListener('click', function (e) {
    var row = e.target.closest('.file-row');
    if (!row) return;
    if (e.target.classList.contains('check')) { updateSelection(); return; }

    var actBtn = e.target.closest('button[data-act]');
    if (actBtn) {
      e.stopPropagation();
      var act = actBtn.dataset.act;
      var path = row.dataset.path;
      if (act === 'open') location.href = '/' + encodePath(path) + '/';
      else if (act === 'download') downloadFile(path);
      else if (act === 'preview') previewFile(path);
      else if (act === 'rename') renameEntry(path, row.dataset.name);
      else if (act === 'delete') deletePaths([path]);
      else if (act === 'share') copyLink(path);
      else if (act === 'send') shareOrSend(path);
      return;
    }

    if (e.target.closest('.file-main')) rowAction(row);
  });

  fileList.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    var row = e.target.closest('.file-row');
    if (!row || e.target !== row) return;
    e.preventDefault();
    rowAction(row);
  });

  // ---------- 鼠标矩形框选 ----------

  var marqueeBox = null;
  var marqueeStart = null;
  var marqueeFrame = 0;

  function updateMarquee(pointer) {
    if (!marqueeBox) return;
    var left = Math.min(marqueeStart.x, pointer.x);
    var top = Math.min(marqueeStart.y, pointer.y);
    var width = Math.abs(pointer.x - marqueeStart.x);
    var height = Math.abs(pointer.y - marqueeStart.y);
    marqueeBox.style.left = left + 'px';
    marqueeBox.style.top = top + 'px';
    marqueeBox.style.width = width + 'px';
    marqueeBox.style.height = height + 'px';

    var marquee = marqueeBox.getBoundingClientRect();
    rows().forEach(function (row) {
      var rect = row.getBoundingClientRect();
      var hit = rect.left < marquee.right && rect.right > marquee.left &&
        rect.top < marquee.bottom && rect.bottom > marquee.top;
      var check = $('.check', row);
      if (check) check.checked = hit;
    });
    updateSelection();
  }

  function requestMarqueeUpdate(pointer) {
    if (marqueeFrame) return;
    marqueeFrame = requestAnimationFrame(function () {
      marqueeFrame = 0;
      updateMarquee(pointer);
    });
  }

  function stopMarquee() {
    document.removeEventListener('pointermove', onMarqueeMove);
    document.removeEventListener('pointerup', stopMarquee);
    document.removeEventListener('keydown', onMarqueeKeydown);
    if (marqueeFrame) {
      cancelAnimationFrame(marqueeFrame);
      marqueeFrame = 0;
    }
    if (marqueeBox) {
      marqueeBox.remove();
      marqueeBox = null;
    }
    marqueeStart = null;
    document.body.classList.remove('marquee-active');
  }

  function onMarqueeKeydown(e) {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    rows().forEach(function (row) {
      var check = $('.check', row);
      if (check) check.checked = false;
    });
    updateSelection();
    stopMarquee();
  }

  function onMarqueeMove(e) {
    if (e.clientY < 32) window.scrollBy(0, -14);
    else if (e.clientY > window.innerHeight - 32) window.scrollBy(0, 14);
    requestMarqueeUpdate({ x: e.clientX, y: e.clientY });
  }

  fileList.addEventListener('pointerdown', function (e) {
    // 只在空白处开始框选；文件本体、复选框和操作按钮保持原行为。
    if (e.pointerType !== 'mouse' || e.button !== 0 || e.target !== fileList) return;
    e.preventDefault();

    rows().forEach(function (row) {
      var check = $('.check', row);
      if (check) check.checked = false;
    });
    updateSelection();

    marqueeStart = { x: e.clientX, y: e.clientY };
    marqueeBox = el('div', { class: 'selection-box', 'aria-hidden': 'true' });
    document.body.appendChild(marqueeBox);
    document.body.classList.add('marquee-active');
    updateMarquee(marqueeStart);

    document.addEventListener('pointermove', onMarqueeMove);
    document.addEventListener('pointerup', stopMarquee);
    document.addEventListener('keydown', onMarqueeKeydown);
  });

  // ---------- 右键菜单 ----------

  var contextMenu = null;

  function hideContextMenu() {
    if (!contextMenu) return;
    contextMenu.remove();
    contextMenu = null;
    document.removeEventListener('pointerdown', onContextMenuOutside, true);
    document.removeEventListener('click', onContextMenuOutside, true);
    document.removeEventListener('scroll', hideContextMenu, true);
    window.removeEventListener('resize', hideContextMenu);
    window.removeEventListener('blur', hideContextMenu);
  }

  function onContextMenuOutside(e) {
    if (contextMenu && !contextMenu.contains(e.target)) hideContextMenu();
  }

  function contextItem(options) {
    if (options.separator) return el('div', { class: 'context-menu-sep', role: 'separator' });
    var item = el('button', {
      type: 'button',
      class: 'context-menu-item' + (options.danger ? ' danger' : ''),
      role: 'menuitem'
    }, [
      el('i', { class: 'fas ' + options.icon, 'aria-hidden': 'true' }),
      el('span', { class: 'context-menu-label', text: options.label })
    ]);
    item.addEventListener('click', function () {
      hideContextMenu();
      options.action();
    });
    return item;
  }

  function showContextMenu(items, x, y) {
    hideContextMenu();
    contextMenu = el('div', { class: 'context-menu', role: 'menu' }, items.map(contextItem));
    document.body.appendChild(contextMenu);

    var rect = contextMenu.getBoundingClientRect();
    contextMenu.style.left = Math.max(8, Math.min(x, window.innerWidth - rect.width - 8)) + 'px';
    contextMenu.style.top = Math.max(8, Math.min(y, window.innerHeight - rect.height - 8)) + 'px';

    document.addEventListener('pointerdown', onContextMenuOutside, true);
    document.addEventListener('click', onContextMenuOutside, true);
    document.addEventListener('scroll', hideContextMenu, true);
    window.addEventListener('resize', hideContextMenu);
    window.addEventListener('blur', hideContextMenu);
  }

  function singleFileMenu(row) {
    var path = row.dataset.path;
    var isDir = row.dataset.dir === '1';
    var items = [];
    if (isDir) {
      items.push({ label: '打开', icon: 'fa-arrow-right', action: function () {
        location.href = '/' + encodePath(path) + '/';
      } });
    } else {
      items.push({ label: '预览', icon: 'fa-eye', action: function () { previewFile(path); } });
      items.push({ label: '下载', icon: 'fa-download', action: function () { downloadFile(path); } });
    }
    items.push({ separator: true });
    items.push({ label: '重命名', icon: 'fa-pen', action: function () { renameEntry(path, row.dataset.name); } });
    items.push({ label: '复制链接', icon: 'fa-link', action: function () { copyLink(path); } });
    items.push({ label: '发送到设备', icon: 'fa-share-nodes', action: function () { shareOrSend(path); } });
    items.push({ separator: true });
    items.push({ label: '删除', icon: 'fa-trash', danger: true, action: function () { deletePaths([path]); } });
    return items;
  }

  function selectedPathsMenu() {
    var paths = selected().map(function (row) { return row.dataset.path; });
    return [
      { label: '下载 ' + paths.length + ' 项', icon: 'fa-download', action: function () { downloadPaths(paths); } },
      { label: '发送 ' + paths.length + ' 项', icon: 'fa-share-nodes', action: function () { openSendPanel(paths); } },
      { separator: true },
      { label: '删除 ' + paths.length + ' 项', icon: 'fa-trash', danger: true, action: function () { deletePaths(paths); } }
    ];
  }

  fileList.addEventListener('contextmenu', function (e) {
    e.preventDefault();
    var row = e.target.closest('.file-row');
    var selectedRows = selected();

    if (row) {
      if (selectedRows.length > 1 && selectedRows.indexOf(row) !== -1) {
        showContextMenu(selectedPathsMenu(), e.clientX, e.clientY);
        return;
      }
      if (row.dataset.action !== 'up') {
        rows().forEach(function (item) {
          var check = $('.check', item);
          if (check) check.checked = item === row;
        });
        updateSelection();
      }
      showContextMenu(singleFileMenu(row), e.clientX, e.clientY);
      return;
    }

    var items = [
      { label: '刷新', icon: 'fa-rotate-right', action: function () { refreshList(); } },
      { separator: true },
      { label: '上传文件', icon: 'fa-file-arrow-up', action: function () { fileInput.click(); } },
      { label: '上传文件夹', icon: 'fa-folder-open', action: function () { folderInput.click(); } },
      { separator: true },
      { label: '新建文件夹', icon: 'fa-folder-plus', action: newFolder }
    ];
    if (selectedRows.length) {
      items.unshift({ separator: true });
      items.unshift({ label: '清除选择', icon: 'fa-xmark', action: function () {
        selectedRows.forEach(function (item) { $('.check', item).checked = false; });
        updateSelection();
      } });
      items.unshift.apply(items, selectedPathsMenu());
    }
    showContextMenu(items, e.clientX, e.clientY);
  });

  $all('.file-row', fileList).forEach(function (r) {
    if (r.dataset.action === 'up') return;
    r.tabIndex = 0;
  });

  $('#btnDeleteSelected').addEventListener('click', function () {
    deletePaths(selected().map(function (r) { return r.dataset.path; }));
  });
  $('#btnDownloadSelected').addEventListener('click', function () {
    downloadPaths(selected().map(function (r) { return r.dataset.path; }));
  });
  $('#btnNewFolder').addEventListener('click', newFolder);

  // ---------- 上传 ----------

  var dropzone = $('#dropzone');
  var fileInput = $('#fileInput');
  var folderInput = $('#folderInput');
  var queueBox = $('#uploadQueue');
  var btnClearQueue = $('#btnClearQueue');

  // pending: { file, relPath, id, status, xhr, itemEl, barEl, metaEl }
  var pending = [];
  var uploading = false;

  $('#btnPickFiles').addEventListener('click', function () { fileInput.click(); });
  $('#btnPickFolder').addEventListener('click', function () { folderInput.click(); });
  dropzone.addEventListener('click', function () { fileInput.click(); });
  dropzone.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); fileInput.click(); }
  });

  ['dragenter', 'dragover'].forEach(function (name) {
    dropzone.addEventListener(name, function (e) {
      e.preventDefault(); e.stopPropagation();
      dropzone.classList.add('dragover');
    });
  });
  ['dragleave', 'drop'].forEach(function (name) {
    dropzone.addEventListener(name, function (e) {
      e.preventDefault(); e.stopPropagation();
      if (name === 'dragleave' && e.relatedTarget && dropzone.contains(e.relatedTarget)) return;
      dropzone.classList.remove('dragover');
    });
  });
  // 阻止浏览器在页面其它位置直接打开拖入的文件
  ['dragover', 'drop'].forEach(function (name) {
    document.body.addEventListener(name, function (e) { e.preventDefault(); });
  });

  dropzone.addEventListener('drop', function (e) {
    var items = e.dataTransfer && e.dataTransfer.items;
    if (items && items.length && items[0].webkitGetAsEntry) {
      collectDataTransfer(items).then(enqueue);
      return;
    }
    if (e.dataTransfer && e.dataTransfer.files) enqueue(Array.from(e.dataTransfer.files));
  });

  // 读取文件夹拖拽：webkitGetAsEntry 可以拿到目录结构
  function collectDataTransfer(items) {
    var out = [];
    var entries = [];
    for (var i = 0; i < items.length; i++) {
      var entry = items[i].webkitGetAsEntry && items[i].webkitGetAsEntry();
      if (entry) entries.push(entry);
    }
    function readEntry(entry, prefix) {
      return new Promise(function (resolve) {
        if (entry.isFile) {
          entry.file(function (file) {
            file.__relPath = prefix + entry.name;
            out.push(file);
            resolve();
          }, function () { resolve(); });
        } else if (entry.isDirectory) {
          var reader = entry.createReader();
          reader.readEntries(function (batch) {
            Promise.all(batch.map(function (child) {
              return readEntry(child, prefix + entry.name + '/');
            })).then(function () { resolve(); });
          }, function () { resolve(); });
        } else {
          resolve();
        }
      });
    }
    return Promise.all(entries.map(function (en) { return readEntry(en, ''); })).then(function () { return out; });
  }

  fileInput.addEventListener('change', function (e) {
    enqueue(Array.from(e.target.files || []));
    fileInput.value = '';
  });

  folderInput.addEventListener('change', function (e) {
    var files = Array.from(e.target.files || []);
    files.forEach(function (f) {
      // webkitRelativePath 形如 "folder/sub/a.txt"：保留顶层目录名，
      // 与拖拽整个文件夹的行为保持一致（都会在 uploads 下创建 "folder/"）。
      f.__relPath = f.webkitRelativePath || f.name;
    });
    enqueue(files);
    folderInput.value = '';
  });

  // 粘贴上传：文本按 .txt 上传，图片/文件按原样上传
  document.addEventListener('paste', function (e) {
    var items = e.clipboardData && e.clipboardData.items;
    if (!items) return;
    var files = [];
    for (var i = 0; i < items.length; i++) {
      if (items[i].kind !== 'file') continue;
      var f = items[i].getAsFile();
      if (!f) continue;
      if (!f.name) {
        var ext = (f.type.split('/')[1] || 'bin').replace('+xml', '');
        f = new File([f], 'paste-' + Date.now() + '.' + ext, { type: f.type });
      }
      files.push(f);
    }
    if (files.length) {
      e.preventDefault();
      toast({ message: '检测到粘贴的内容，开始上传…', kind: 'info' });
      enqueue(files);
    }
  });

  function enqueue(files) {
    if (!files || !files.length) return;
    var known = {};
    pending.forEach(function (p) { known[p.relPath] = true; });
    files.forEach(function (f) {
      var rel = f.__relPath || f.name;
      rel = rel.replace(/^\/+/, '');
      if (!rel || known[rel]) return;
      known[rel] = true;
      pending.push({ file: f, relPath: rel, status: 'queued' });
    });
    renderQueue();
    if (!uploading) uploadLoop();
  }

  function renderQueue() {
    // 只渲染还没完成的条目；完成/失败的保留展示
    queueBox.textContent = '';
    if (!pending.length) {
      btnClearQueue.classList.add('hidden');
      return;
    }
    btnClearQueue.classList.remove('hidden');
    pending.forEach(function (p) {
      var bar = el('i');
      var meta = el('span', { class: 'meta', text: p.status === 'queued' ? '等待中' : (p.status === 'failed' ? p.error || '失败' : '') });
      var nameEl = el('span', { class: 'name', title: p.relPath, text: p.relPath });
      var cancelBtn = el('button', {
        class: 'icon-btn ghost', title: '取消上传', 'aria-label': '取消上传',
        style: 'width:26px;height:26px;font-size:.75rem',
        onclick: function (ev) {
          ev.stopPropagation();
          if (p.xhr) { p.xhr.abort(); }
          else { p.status = 'failed'; p.error = '已取消'; renderQueueItemState(p); }
        }
      }, [el('i', { class: 'fas fa-xmark' })]);
      var item = el('div', { class: 'queue-item' + (p.status === 'done' ? ' done' : '') + (p.status === 'failed' ? ' failed' : '') }, [
        el('i', { class: 'fas fa-file', style: 'color:var(--text-muted)' }),
        nameEl,
        el('span', { style: 'display:flex;align-items:center;gap:8px' }, [meta, cancelBtn]),
        el('div', { class: 'bar' }, [bar])
      ]);
      p.itemEl = item;
      p.barEl = bar;
      p.metaEl = meta;
      queueBox.appendChild(item);
      if (p.status === 'uploading' || p.status === 'done' || p.status === 'failed') {
        bar.style.width = ((p.progress || 0) * 100).toFixed(1) + '%';
      }
      cancelBtn.classList.toggle('hidden', p.status !== 'uploading' && p.status !== 'queued');
    });
  }

  btnClearQueue.addEventListener('click', function () {
    pending = pending.filter(function (p) { return p.status === 'uploading'; });
    renderQueue();
  });

  // 单文件一条请求（XHR），这样每个文件都有独立进度，失败互不影响。
  // 超过阈值的文件走分片上传（可断点续传）。
  function uploadOne(item) {
    if (item.file.size > CHUNK_THRESHOLD) return uploadChunked(item);
    return new Promise(function (resolve) {
      item.status = 'uploading';
      renderQueue();

      var fd = new FormData();
      // 目录结构：relPath 决定文件在 uploads 下的最终位置
      fd.append('relPath', item.relPath);
      fd.append('conflict', 'rename');
      fd.append('file', item.file, item.file.name);

      var xhr = new XMLHttpRequest();
      item.xhr = xhr;
      var started = Date.now();

      xhr.open('POST', '/api/web/upload?path=' + encodeURIComponent(CURRENT_PATH), true);
      xhr.setRequestHeader('X-Web-Token', TOKEN);

      xhr.upload.onprogress = function (ev) {
        if (!ev.lengthComputable) return;
        var ratio = ev.loaded / ev.total;
        item.progress = ratio;
        if (item.barEl) item.barEl.style.width = (ratio * 100).toFixed(1) + '%';
        if (item.metaEl) {
          var secs = (Date.now() - started) / 1000;
          var speed = secs > 0.2 ? ev.loaded / secs : 0;
          item.metaEl.textContent = Math.round(ratio * 100) + '%' +
            (speed ? ' · ' + humanSize(speed) + '/s' : '');
        }
      };

      xhr.onload = function () {
        if (xhr.status >= 200 && xhr.status < 300) {
          item.status = 'done';
          item.progress = 1;
          if (item.barEl) item.barEl.style.width = '100%';
          if (item.metaEl) item.metaEl.textContent = '完成';
          resolve({ ok: true });
        } else {
          var msg = 'HTTP ' + xhr.status;
          try { var d = JSON.parse(xhr.responseText); if (d && d.message) msg = d.message; } catch (e) {}
          item.status = 'failed';
          item.error = msg;
          if (item.metaEl) item.metaEl.textContent = msg;
          resolve({ ok: false, message: msg });
        }
        renderQueueItemState(item);
      };

      xhr.onerror = function () {
        item.status = 'failed';
        item.error = '网络错误';
        if (item.metaEl) item.metaEl.textContent = '网络错误';
        renderQueueItemState(item);
        resolve({ ok: false, message: '网络错误' });
      };

      xhr.onabort = function () {
        item.status = 'failed';
        item.error = '已取消';
        resolve({ ok: false, message: '已取消' });
      };

      xhr.send(fd);
    });
  }

  // 分片上传：uploadId 存在 localStorage 里，刷新后同一文件可续传。
  function uploadChunked(item) {
    var file = item.file;
    var key = 'ls-chunk-' + item.relPath + '-' + file.size + '-' + (file.lastModified || 0);
    var uploadId = '';
    try {
      uploadId = localStorage.getItem(key) || '';
    } catch (e) {}
    if (!/^[0-9a-f]{32}$/.test(uploadId)) uploadId = newUploadId();

    item.status = 'uploading';
    renderQueue();
    var started = Date.now();

    var base = '/api/web/upload/chunk?uploadId=' + uploadId +
      '&path=' + encodeURIComponent(CURRENT_PATH) +
      '&relPath=' + encodeURIComponent(item.relPath) +
      '&name=' + encodeURIComponent(file.name) +
      '&total=' + file.size + '&conflict=rename';

    function send(offset) {
      return new Promise(function (resolve, reject) {
        var end = Math.min(offset + CHUNK_SIZE, file.size);
        var xhr = new XMLHttpRequest();
        item.xhr = xhr;
        xhr.open('POST', base + '&offset=' + offset, true);
        xhr.setRequestHeader('X-Web-Token', TOKEN);
        xhr.setRequestHeader('Content-Type', 'application/octet-stream');

        xhr.onload = function () {
          var data = {};
          try { data = JSON.parse(xhr.responseText || '{}'); } catch (e) {}
          if (xhr.status === 409 && typeof data.offset === 'number') {
            // 前端状态与服务器不一致：以服务器为准继续
            item.progress = file.size ? data.offset / file.size : 0;
            resolve({ offset: data.offset });
            return;
          }
          if (xhr.status >= 200 && xhr.status < 300) {
            item.progress = file.size ? (data.done ? 1 : end / file.size) : 1;
            if (item.barEl) item.barEl.style.width = (item.progress * 100).toFixed(1) + '%';
            var secs = (Date.now() - started) / 1000;
            if (item.metaEl) {
              item.metaEl.textContent = Math.round(item.progress * 100) + '%' +
                (secs > 0.3 ? ' · ' + humanSize(end / secs) + '/s' : '');
            }
            resolve(data.done ? { done: true, data: data } : { done: false, offset: end });
            return;
          }
          reject(new Error(data.message || ('HTTP ' + xhr.status)));
        };
        xhr.onerror = function () { reject(new Error('网络错误')); };
        xhr.onabort = function () { reject(new Error('已取消')); };
        xhr.send(file.slice(offset, end));
      });
    }

    function loop(offset) {
      return send(offset).then(function (res) {
        if (res.done) return res;
        return loop(res.offset);
      });
    }

    return loop(0).then(function (res) {
      item.status = 'done';
      item.progress = 1;
      if (item.barEl) item.barEl.style.width = '100%';
      if (item.metaEl) item.metaEl.textContent = '完成';
      try { localStorage.removeItem(key); } catch (e) {}
      renderQueueItemState(item);
      return { ok: true, data: res.data };
    }).catch(function (err) {
      item.status = 'failed';
      item.error = err.message;
      if (item.metaEl) item.metaEl.textContent = err.message;
      renderQueueItemState(item);
      // 取消时清掉服务端 part 文件与本地记录
      if (err.message === '已取消') {
        try { localStorage.removeItem(key); } catch (e) {}
        fetch('/api/web/upload/chunk?uploadId=' + uploadId +
          '&path=' + encodeURIComponent(CURRENT_PATH), {
          method: 'DELETE', headers: { 'X-Web-Token': TOKEN }
        }).catch(function () {});
      }
      return { ok: false, message: err.message };
    });
  }

  function newUploadId() {
    var bytes = new Uint8Array(16);
    if (window.crypto && window.crypto.getRandomValues) {
      window.crypto.getRandomValues(bytes);
    } else {
      for (var i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
    }
    return Array.prototype.map.call(bytes, function (b) {
      return ('0' + b.toString(16)).slice(-2);
    }).join('');
  }

  function renderQueueItemState(item) {
    if (!item.itemEl) return;
    item.itemEl.classList.toggle('done', item.status === 'done');
    item.itemEl.classList.toggle('failed', item.status === 'failed');
  }

  var CONCURRENCY = 2;

  // 大文件走分片上传：4 MiB 一片，可续传
  var CHUNK_SIZE = 4 * 1024 * 1024;
  var CHUNK_THRESHOLD = 16 * 1024 * 1024;

  function uploadLoop() {
    uploading = true;
    var okCount = 0, failCount = 0, totalBytes = 0;
    var active = 0;

    function next() {
      while (active < CONCURRENCY) {
        var item = pending.filter(function (p) { return p.status === 'queued'; })[0];
        if (!item) break;
        active++;
        totalBytes += item.file.size;
        uploadOne(item).then(function (res) {
          if (res.ok) okCount++; else failCount++;
          active--;
          next();
        });
      }
      if (active === 0) finish();
    }

    function finish() {
      uploading = false;
      if (okCount) {
        toast({ message: '已上传 ' + okCount + ' 个文件（' + humanSize(totalBytes) + '）', kind: 'success' });
      }
      if (failCount) {
        toast({
          message: failCount + ' 个文件上传失败：' + firstError(),
          kind: 'error',
          duration: 7000
        });
      }
      if (okCount) {
        // 局部刷新列表，比整页 reload 更平滑
        setTimeout(function () { refreshList(); }, 300);
      }
      // 保留已完成/失败条目 4 秒后清理，让用户能看到结果
      setTimeout(function () {
        pending = pending.filter(function (p) { return p.status === 'uploading'; });
        renderQueue();
      }, 4000);
    }

    function firstError() {
      var f = pending.filter(function (p) { return p.status === 'failed'; })[0];
      return f ? f.relPath + '：' + f.error : '未知原因';
    }

    next();
  }

  // ---------- 列表刷新 ----------

  function refreshList() {
    return fetch('/api/web/files?path=' + encodeURIComponent(CURRENT_PATH), { credentials: 'same-origin' })
      .then(function (r) {
        if (!r.ok) return apiError(r, '刷新失败').then(function (m) { throw new Error(m); });
        return r.json();
      })
      .then(function (data) { renderEntries(data.entries || []); })
      .catch(function (e) { toast({ message: e.message || '刷新失败', kind: 'error' }); });
  }

  function renderEntries(entries) {
    var existing = {};
    rows().forEach(function (r) { existing[r.dataset.path] = true; });
    $all('.file-row', fileList).forEach(function (r) {
      if (r.dataset.action !== 'up') r.remove();
    });
    entries.forEach(function (e) {
      var row = buildRow(e);
      // 刷新后新增的行（上传 / 新建文件夹）加淡入动画
      if (!existing[e.path]) row.classList.add('row-enter');
      fileList.appendChild(row);
    });
    applySort();
    applyFilter();
    updateSelection();
    $('#emptyState').classList.toggle('hidden', entries.length > 0);
  }

  function buildRow(e) {
    var isDir = !!e.isDir;
    var icon = isDir ? 'fa-folder' :
      e.mime && e.mime.indexOf('image/') === 0 ? 'fa-file-image' :
      e.mime && e.mime.indexOf('video/') === 0 ? 'fa-file-video' :
      e.mime && e.mime.indexOf('audio/') === 0 ? 'fa-file-audio' :
      e.mime === 'application/pdf' ? 'fa-file-pdf' :
      e.mime === 'application/zip' ? 'fa-file-archive' : 'fa-file';

    var actions = el('span', { class: 'file-actions' });
    if (!isDir) {
      actions.appendChild(actionBtn('preview', 'fa-eye', '预览'));
      actions.appendChild(actionBtn('download', 'fa-download', '下载'));
    } else {
      actions.appendChild(actionBtn('open', 'fa-arrow-right', '打开'));
    }
    actions.appendChild(actionBtn('rename', 'fa-pen', '重命名'));
    actions.appendChild(actionBtn('share', 'fa-link', '复制链接'));
    actions.appendChild(actionBtn('send', 'fa-share-nodes', '发送到设备'));
    var delBtn = actionBtn('delete', 'fa-trash', '删除');
    delBtn.classList.add('danger');
    actions.appendChild(delBtn);

    var row = el('li', {
      class: 'file-row' + (isDir ? ' dir' : ''),
      tabindex: '0',
      'data-name': e.name,
      'data-path': e.path,
      'data-dir': isDir ? '1' : '0',
      'data-size': e.size,
      'data-time': e.modTime,
      'data-mime': e.mime || ''
    }, [
      el('input', { type: 'checkbox', class: 'check', 'aria-label': '选择 ' + e.name }),
      el('div', { class: 'file-main' }, [
        el('span', { class: 'file-icon' }, [el('i', { class: 'fas ' + icon })]),
        el('div', { style: 'min-width:0' }, [
          el('div', { class: 'file-name', title: e.name, text: e.name }),
          el('div', { class: 'file-sub', text: isDir ? '文件夹' : fmtTime(e.modTime) })
        ])
      ]),
      el('span', { class: 'file-size', text: isDir ? '' : humanSize(e.size) }),
      actions
    ]);
    applyThumb(row);
    return row;
  }

  function actionBtn(act, icon, title) {
    return el('button', { class: 'icon-btn ghost', 'data-act': act, title: title }, [
      el('i', { class: 'fas ' + icon })
    ]);
  }

  $('#btnRefresh').addEventListener('click', function () {
    refreshList();
    toast({ message: '已刷新', kind: 'info', duration: 1800 });
  });

  // ---------- 发送到局域网设备 ----------

  var sendPanelOpen = false;
  var sendSelectedPaths = [];

  function openSendPanel(paths) {
    sendSelectedPaths = paths || [];
    var refreshTimer;
    var isLoading = false;
    var body = el('div');
    var devicesBox = el('div', { class: 'device-list' });
    body.appendChild(el('p', {
      style: 'margin-top:0;color:var(--text-muted);font-size:.86rem',
      text: sendSelectedPaths.length
        ? '发送 ' + sendSelectedPaths.length + ' 项到：'
        : '选择要发送到哪台设备（发送当前目录的全部内容）：'
    }));
    body.appendChild(devicesBox);

    var refreshBtn = el('button', { class: 'btn sm device-refresh', style: 'margin-top:12px' }, [
      el('i', { class: 'fas fa-rotate' }), document.createTextNode(' 重新扫描')
    ]);
    body.appendChild(refreshBtn);

    function loadDevices() {
      if (isLoading) return;
      isLoading = true;
      refreshBtn.classList.add('scanning');
      refreshBtn.disabled = true;
      var hasDevices = devicesBox.querySelector('.device') !== null;
      if (!hasDevices) {
        devicesBox.textContent = '';
        devicesBox.appendChild(el('div', { class: 'empty', style: 'padding:20px', text: '正在扫描局域网设备…' }));
      }
      fetch('/api/web/devices', { credentials: 'same-origin' })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          var list = (data && data.devices) || [];
          devicesBox.textContent = '';
          if (!list.length) {
            devicesBox.appendChild(el('div', {
              class: 'empty', style: 'padding:20px',
              text: '没有发现设备。请确认对方已打开 LocalSend，并允许局域网发现。'
            }));
            return;
          }
          list.forEach(function (d) {
            var icon = d.deviceType === 'mobile' ? 'fa-mobile-screen' :
              d.deviceType === 'web' ? 'fa-globe' : 'fa-desktop';
            devicesBox.appendChild(el('button', {
              class: 'device',
              onclick: function () { startSend(d); }
            }, [
              el('span', { class: 'dev-icon' }, [el('i', { class: 'fas ' + icon })]),
              el('span', { style: 'min-width:0' }, [
                el('div', { class: 'dev-name', text: d.alias || d.ip }),
                el('div', { class: 'dev-meta', text: (d.deviceModel || d.deviceType || '') + ' · ' + d.ip + (d.port ? ':' + d.port : '') })
              ])
            ]));
          });
        })
        .catch(function () {
          if (!hasDevices) {
            devicesBox.textContent = '';
            devicesBox.appendChild(el('div', { class: 'empty', style: 'padding:20px', text: '扫描设备失败' }));
          }
        })
        .finally(function () {
          isLoading = false;
          refreshBtn.classList.remove('scanning');
          refreshBtn.disabled = false;
        });
    }

    refreshBtn.addEventListener('click', function (e) { e.stopPropagation(); loadDevices(); });

    openModal({
      title: '发送到设备',
      body: body,
      onClose: function () {
        clearInterval(refreshTimer);
        sendPanelOpen = false;
      },
      buttons: [{ label: '关闭' }]
    });
    sendPanelOpen = true;
    loadDevices();
    refreshTimer = setInterval(loadDevices, 2000);
  }

  function startSend(device) {
    var paths = sendSelectedPaths.length
      ? sendSelectedPaths.slice()
      : (CURRENT_PATH ? [CURRENT_PATH] : []);
    if (!paths.length) {
      toast({ message: '请先勾选要发送的文件，或进入某个目录后再发送', kind: 'warn' });
      return;
    }
    var payload = { device: device, paths: paths };

    modalBody.textContent = '';
    modalBody.appendChild(el('p', {
      text: '正在发送到 ' + (device.alias || device.ip) + '…',
      style: 'margin-top:0'
    }));
    var jobBox = el('div', { class: 'job-box' });
    modalBody.appendChild(jobBox);
    modalFoot.textContent = '';
    var cancelBtn = el('button', { class: 'btn danger' }, [
      el('i', { class: 'fas fa-stop' }), document.createTextNode(' 取消发送')
    ]);
    modalFoot.appendChild(cancelBtn);
    modalFoot.appendChild(el('button', {
      class: 'btn', text: '关闭', onclick: closeModal
    }));

    fetch('/api/web/send', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Web-Token': TOKEN },
      body: JSON.stringify(payload)
    }).then(function (r) {
      if (!r.ok) return apiError(r, '发送失败').then(function (m) { throw new Error(m); });
      return r.json();
    }).then(function (data) {
      var jobId = data.id;
      var timer = setInterval(function () { poll(); }, 700);

      function stopPolling() { clearInterval(timer); }

      cancelBtn.addEventListener('click', function () {
        fetch('/api/web/send/cancel?id=' + encodeURIComponent(jobId), {
          method: 'POST', headers: { 'X-Web-Token': TOKEN }
        }).catch(function () {});
        cancelBtn.disabled = true;
      });

      function poll() {
        fetch('/api/web/send/status?id=' + encodeURIComponent(jobId), { credentials: 'same-origin' })
          .then(function (r) { return r.json(); })
          .then(function (st) {
            renderJob(jobBox, st);
            if (st.state === 'done' || st.state === 'error') {
              stopPolling();
              cancelBtn.classList.add('hidden');
              if (st.state === 'done') toast({ message: '发送完成', kind: 'success' });
              else toast({ message: '发送失败：' + (st.message || '未知原因'), kind: 'error' });
            }
          })
          .catch(function () { stopPolling(); });
      }
      poll();
    }).catch(function (e) {
      toast({ message: e.message || '操作失败', kind: 'error' });
      jobBox.appendChild(el('div', { class: 'empty', text: e.message }));
    });
  }

  function renderJob(box, st) {
    box.textContent = '';
    var percent = st.total > 0 ? Math.min(100, (st.sent / st.total) * 100) : (st.state === 'done' ? 100 : 0);
    box.appendChild(el('div', {
      style: 'display:flex;justify-content:space-between;font-size:.82rem;color:var(--text-muted)'
    }, [
      el('span', { text: st.state === 'preparing' ? '准备中…' : st.state === 'transferring' ? '传输中' : st.state === 'done' ? '已完成' : '失败' }),
      el('span', { text: humanSize(st.sent || 0) + ' / ' + humanSize(st.total || 0) + ' · ' + percent.toFixed(0) + '%' })
    ]));
    (st.files || []).forEach(function (f) {
      var p = f.total > 0 ? Math.min(100, (f.done / f.total) * 100) : 0;
      box.appendChild(el('div', { class: 'job-file' }, [
        el('div', { text: f.name + '  ' + humanSize(f.done || 0) + ' / ' + humanSize(f.total || 0) }),
        el('div', { class: 'bar' }, [el('i', { style: 'width:' + p + '%' })])
      ]));
    });
    if (st.message && st.state === 'error') {
      box.appendChild(el('div', { class: 'empty', style: 'padding:12px', text: st.message }));
    }
  }

  $('#btnSendPanel').addEventListener('click', function () { openSendPanel([]); });

  // ---------- 传输记录 ----------

  $('#btnHistory').addEventListener('click', function () {
    var body = el('div');
    var list = el('div', { class: 'device-list' });
    body.appendChild(list);
    openModal({ title: '传输记录', body: body, buttons: [{ label: '关闭' }] });

    fetch('/api/web/history', { credentials: 'same-origin' })
      .then(function (r) { return r.json(); })
      .then(function (data) {
        var items = (data && data.entries) || [];
        if (!items.length) {
          list.appendChild(el('div', { class: 'empty', style: 'padding:20px', text: '还没有传输记录。' }));
          return;
        }
        items.forEach(function (e) {
          var incoming = e.direction === 'receive';
          list.appendChild(el('div', {
            class: 'device', style: 'cursor:default'
          }, [
            el('span', { class: 'dev-icon' }, [
              el('i', { class: 'fas ' + (incoming ? 'fa-arrow-down' : 'fa-arrow-up') })
            ]),
            el('span', { style: 'min-width:0;flex:1' }, [
              el('div', { class: 'dev-name', text: (incoming ? '接收 ' : '发送 ') + e.fileName }),
              el('div', {
                class: 'dev-meta',
                text: (e.peer || '未知来源') + ' · ' + humanSize(e.size) + ' · ' + (e.at || '')
              })
            ]),
            el('span', {
              class: 'tag',
              text: e.ok ? '成功' : ('失败' + (e.error ? '：' + e.error : ''))
            })
          ]));
        });
      })
      .catch(function () {
        list.textContent = '';
        list.appendChild(el('div', { class: 'empty', style: 'padding:20px', text: '读取传输记录失败' }));
      });
  });

  // ---------- 分享本页（二维码） ----------

  $('#btnSharePage').addEventListener('click', function () {
    var body = el('div', { style: 'text-align:center' });
    var img = el('img', {
      alt: '二维码',
      style: 'width:min(260px,70vw);height:auto;border-radius:12px;background:#fff;padding:8px'
    });
    var link = el('div', {
      style: 'margin-top:10px;font-size:.8rem;color:var(--text-muted);word-break:break-all'
    });
    var copyBtn = el('button', { class: 'btn sm', style: 'margin-top:10px' }, [
      el('i', { class: 'fas fa-link' }), document.createTextNode(' 复制链接')
    ]);
    body.appendChild(img);
    body.appendChild(link);
    body.appendChild(copyBtn);

    openModal({ title: '扫码打开 / 分享', body: body, buttons: [{ label: '关闭' }] });

    fetch('/api/web/share', { credentials: 'same-origin' })
      .then(function (r) { return r.json(); })
      .then(function (data) {
        var url = (data.urls && data.urls[0]) || location.origin + '/';
        link.textContent = url + (data.pinEnabled ? '（对方需要输入 PIN）' : '');
        img.src = '/api/web/qr?url=' + encodeURIComponent(url);
        copyBtn.addEventListener('click', function () {
          if (navigator.clipboard && window.isSecureContext) {
            navigator.clipboard.writeText(url).then(function () { toast({ message: '链接已复制', kind: 'success' }); });
          } else {
            fallbackCopy(url, function () { toast({ message: '链接已复制', kind: 'success' }); });
          }
        });
      })
      .catch(function () {
        link.textContent = '读取分享地址失败';
      });
  });

  // ---------- 键盘快捷键 ----------

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') { closeModal(); return; }
    var tag = (e.target.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
    if (e.key === '/') { e.preventDefault(); searchInput.focus(); }
  });
})();
