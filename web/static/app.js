// Wink Dashboard - Vanilla JS Client
(function () {
  'use strict';

  // --- i18n helper ---
  var I18N = window.I18N || {};
  function t(key) { return I18N[key] || key; }

  // --- CSS custom property values ---
  var BAR_WIDTH = 8;
  var BAR_GAP = 3;

  // --- State ---
  var selectedMonitorId = null;
  var monitors = [];
  var listPollTimer = null;
  var detailPollTimer = null;
  var POLL_INTERVAL = 10000;
  var isPageVisible = true;
  var collapsedGroups = {}; // track collapsed group IDs
  var sortMode = false;
  var lastGroupOrder = [];

  // --- Theme ---
  function getThemeCookie() {
    var m = document.cookie.match(/(?:^|;\s*)wink_theme=([^;]*)/);
    return m ? m[1] : 'light';
  }

  function applyTheme(theme) {
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }

  function toggleTheme() {
    var current = getThemeCookie();
    var next = current === 'dark' ? 'light' : 'dark';
    // Set cookie via server to keep it consistent
    fetch('/theme?t=' + next, { credentials: 'same-origin' }).then(function () {
      applyTheme(next);
    });
  }

  // --- Utility ---
  function timeAgo(unixSec) {
    if (!unixSec) return '-';
    var diff = Math.floor(Date.now() / 1000) - unixSec;
    if (diff < 0) diff = 0;
    if (diff < 60) return diff + 's ago';
    if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    return Math.floor(diff / 86400) + 'd ago';
  }

  function formatDuration(seconds) {
    if (seconds < 60) return seconds + 's';
    if (seconds < 3600) return Math.floor(seconds / 60) + 'm ' + (seconds % 60) + 's';
    var h = Math.floor(seconds / 3600);
    var m = Math.floor((seconds % 3600) / 60);
    return h + 'h ' + m + 'm';
  }

  function uptimeClass(val) {
    if (val >= 99) return 'uptime-good';
    if (val >= 95) return 'uptime-warn';
    return 'uptime-bad';
  }

  // --- Heartbeat Bars ---
  function calcBarCount(container) {
    var w = container.clientWidth;
    // Subtract px-4 padding (32px) for list containers
    if (container.id === 'monitor-list') {
      w -= 32;
    }
    if (w <= 0) return 30;
    // CSS gap is only between bars, so N bars occupy N*(W) + (N-1)*G = N*(W+G) - G
    return Math.floor((w + BAR_GAP) / (BAR_WIDTH + BAR_GAP));
  }

  function renderBars(container, points, barCount) {
    container.innerHTML = '';
    var frag = document.createDocumentFragment();

    // Gray padding on the left for missing data
    var padCount = Math.max(0, barCount - points.length);
    for (var i = 0; i < padCount; i++) {
      var pad = document.createElement('div');
      pad.className = 'heartbeat-bar heartbeat-bar--empty';
      frag.appendChild(pad);
    }

    // Actual data bars (take the last barCount points)
    var start = Math.max(0, points.length - barCount);
    for (var j = start; j < points.length; j++) {
      var bar = document.createElement('div');
      var cls = 'heartbeat-bar ' + (points[j].up ? 'heartbeat-bar--up' : 'heartbeat-bar--down');
      if (j === points.length - 1) cls += ' heartbeat-bar--new';
      bar.className = cls;
      bar.title = points[j].v + 'ms - ' + new Date(points[j].t * 1000).toLocaleTimeString();
      frag.appendChild(bar);
    }

    container.appendChild(frag);
  }

  // --- API ---
  function fetchJSON(url, cb) {
    fetch(url, { credentials: 'same-origin' })
      .then(function (res) {
        if (res.status === 401) {
          window.location.href = '/login';
          return;
        }
        return res.json();
      })
      .then(function (data) { if (data) cb(null, data); })
      .catch(function (err) { cb(err, null); });
  }

  // --- Monitor List ---
  function refreshList() {
    var listContainer = document.getElementById('monitor-list');
    if (!listContainer) return;

    // Calculate points based on list container width
    var barCount = calcBarCount(listContainer);

    fetchJSON('/api/monitors?points=' + barCount, function (err, data) {
      if (err || !data) return;

      monitors = data.monitors || [];
      lastGroupOrder = data.group_order || [];

      listContainer.innerHTML = '';

      if (monitors.length === 0) {
        sortMode = false;
        var empty = document.createElement('div');
        empty.className = 'flex flex-col items-center justify-center py-16 text-gray-400';
        empty.innerHTML = '<p class="text-lg mb-2">' + t('dash.no_monitors') + '</p>' +
          '<a href="/monitors/new" class="text-blue-500 hover:text-blue-400">' + t('dash.add_first') + '</a>';
        listContainer.appendChild(empty);
        return;
      }

      // Sort toggle bar
      var sortBar = document.createElement('div');
      sortBar.className = 'flex items-center justify-end px-3 py-1 border-b border-gray-100 dark:border-gray-800';
      var sortBtn = document.createElement('button');
      sortBtn.className = 'p-1.5 rounded transition-colors ' +
        (sortMode
          ? 'text-blue-600 bg-blue-50 dark:text-blue-400 dark:bg-blue-900/30'
          : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700');
      sortBtn.innerHTML = '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/></svg>';
      sortBtn.title = t('dash.sort');
      sortBtn.addEventListener('click', function() { sortMode = !sortMode; refreshList(); });
      sortBar.appendChild(sortBtn);
      listContainer.appendChild(sortBar);

      // Group monitors by group_id
      var groups = {};
      var ungrouped = [];
      for (var i = 0; i < monitors.length; i++) {
        var m = monitors[i];
        if (m.group_id) {
          if (!groups[m.group_id]) {
            groups[m.group_id] = { name: m.group_name || m.group_id, items: [] };
          }
          groups[m.group_id].items.push(m);
        } else {
          ungrouped.push(m);
        }
      }

      var frag = document.createDocumentFragment();

      // Render grouped monitors (use server-provided order)
      var groupIds = (data.group_order && data.group_order.length > 0)
        ? data.group_order.filter(function(id) { return groups[id]; })
        : Object.keys(groups);
      for (var g = 0; g < groupIds.length; g++) {
        var grp = groups[groupIds[g]];
        var header = document.createElement('div');
        header.className = 'group-header flex items-center gap-2 px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-800/50 cursor-pointer select-none border-b border-gray-100 dark:border-gray-800';
        var headerHtml = '<svg class="w-4 h-4 transition-transform group-arrow" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7"/></svg>' +
          '<span class="flex-1">' + escapeHtml(grp.name) + '</span>';
        if (sortMode) {
          headerHtml += '<span class="flex items-center gap-0.5 ml-auto">' +
            '<button class="sort-grp-up p-0.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"><svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7"/></svg></button>' +
            '<button class="sort-grp-down p-0.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"><svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7"/></svg></button>' +
            '</span>';
        }
        header.innerHTML = headerHtml;
        header.setAttribute('data-group', groupIds[g]);
        if (collapsedGroups[groupIds[g]]) {
          var arrow = header.querySelector('.group-arrow');
          if (arrow) arrow.classList.add('rotate-180');
        }
        header.addEventListener('click', (function(gid) {
          return function() {
            collapsedGroups[gid] = !collapsedGroups[gid];
            var items = listContainer.querySelectorAll('.monitor-item[data-group="' + gid + '"]');
            var arrow = this.querySelector('.group-arrow');
            for (var k = 0; k < items.length; k++) {
              items[k].classList.toggle('hidden');
            }
            if (arrow) arrow.classList.toggle('rotate-180');
          };
        })(groupIds[g]));
        if (sortMode) {
          (function(gid) {
            var grpUp = header.querySelector('.sort-grp-up');
            var grpDown = header.querySelector('.sort-grp-down');
            if (grpUp) grpUp.addEventListener('click', function(e) { e.stopPropagation(); moveGroup(gid, -1); });
            if (grpDown) grpDown.addEventListener('click', function(e) { e.stopPropagation(); moveGroup(gid, 1); });
          })(groupIds[g]);
        }
        frag.appendChild(header);
        for (var j = 0; j < grp.items.length; j++) {
          var item = createMonitorItem(grp.items[j], barCount);
          item.setAttribute('data-group', groupIds[g]);
          if (collapsedGroups[groupIds[g]]) item.classList.add('hidden');
          frag.appendChild(item);
        }
      }

      // Render ungrouped monitors
      if (ungrouped.length > 0 && groupIds.length > 0) {
        var uHeader = document.createElement('div');
        uHeader.className = 'group-header flex items-center gap-2 px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-800/50 cursor-pointer select-none border-b border-gray-100 dark:border-gray-800';
        uHeader.innerHTML = '<svg class="w-4 h-4 transition-transform group-arrow" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7"/></svg>' + escapeHtml(t('dash.ungrouped'));
        if (collapsedGroups['_ungrouped']) {
          var uArrow = uHeader.querySelector('.group-arrow');
          if (uArrow) uArrow.classList.add('rotate-180');
        }
        uHeader.addEventListener('click', function() {
          collapsedGroups['_ungrouped'] = !collapsedGroups['_ungrouped'];
          var items = listContainer.querySelectorAll('.monitor-item[data-group="_ungrouped"]');
          var arrow = this.querySelector('.group-arrow');
          for (var k = 0; k < items.length; k++) {
            items[k].classList.toggle('hidden');
          }
          if (arrow) arrow.classList.toggle('rotate-180');
        });
        frag.appendChild(uHeader);
      }
      for (var u = 0; u < ungrouped.length; u++) {
        var uItem = createMonitorItem(ungrouped[u], barCount);
        uItem.setAttribute('data-group', '_ungrouped');
        if (collapsedGroups['_ungrouped']) uItem.classList.add('hidden');
        frag.appendChild(uItem);
      }

      listContainer.appendChild(frag);
    });
  }

  function createMonitorItem(m, barCount) {
    var item = document.createElement('div');
    item.className = 'monitor-item cursor-pointer border-b border-gray-100 dark:border-gray-800 px-4 py-3.5 hover:bg-gray-50 dark:hover:bg-gray-800/50';
    item.setAttribute('data-id', m.id);

    if (m.id === selectedMonitorId) {
      item.classList.add('monitor-item--selected');
    }

    // Status dot color
    var dotColor = 'bg-gray-400';
    var dotClass = '';
    if (!m.enabled) {
      dotColor = 'bg-gray-400';
      dotClass = '';
    } else if (m.has_history) {
      dotColor = m.is_up ? 'bg-green-500' : 'bg-red-500';
      dotClass = m.is_up ? '' : ' status-dot--down';
    }

    var html = '';

    if (sortMode) {
      html += '<div class="flex items-center gap-2">';
      html += '<div class="flex flex-col flex-shrink-0">';
      html += '<button class="sort-mon-up p-0.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"><svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7"/></svg></button>';
      html += '<button class="sort-mon-down p-0.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"><svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7"/></svg></button>';
      html += '</div>';
      html += '<div class="flex-1 min-w-0">';
    }

    // Top row: status dot, name, uptime badges
    html += '<div class="flex items-center justify-between mb-2">' +
      '<div class="flex items-center gap-2 min-w-0">' +
        '<span class="w-2.5 h-2.5 rounded-full flex-shrink-0 ' + dotColor + dotClass + '"></span>' +
        '<span class="font-medium text-gray-900 dark:text-white truncate">' + escapeHtml(m.name) + '</span>' +
        '<span class="text-xs text-gray-400 dark:text-gray-500 flex-shrink-0">' + m.type.toUpperCase() + '</span>' +
        (!m.enabled ? '<span class="text-xs px-1.5 py-0.5 rounded bg-gray-200 dark:bg-gray-700 text-gray-500 dark:text-gray-400 flex-shrink-0">' + t('dash.status_paused') + '</span>' : '') +
      '</div>' +
      '<div class="flex items-center gap-3 text-xs flex-shrink-0">';

    if (m.has_history) {
      html += '<span class="' + uptimeClass(m.uptime_24h) + '">' + m.uptime_24h.toFixed(2) + '%</span>';
    }
    html += '</div></div>';

    // Bottom row: heartbeat bars
    html += '<div class="heartbeat-container" style="height:var(--bar-height-list)"></div>';

    if (sortMode) {
      html += '</div></div>';
    }

    item.innerHTML = html;

    // Render heartbeat bars into the container
    var barsContainer = item.querySelector('.heartbeat-container');
    renderBars(barsContainer, m.heartbeats || [], barCount);

    // Sort button handlers
    if (sortMode) {
      var monUp = item.querySelector('.sort-mon-up');
      var monDown = item.querySelector('.sort-mon-down');
      if (monUp) monUp.addEventListener('click', function(e) { e.stopPropagation(); moveMonitor(m.id, -1); });
      if (monDown) monDown.addEventListener('click', function(e) { e.stopPropagation(); moveMonitor(m.id, 1); });
    }

    // Click handler
    item.addEventListener('click', function () {
      selectMonitor(m.id);
    });

    return item;
  }

  function escapeHtml(str) {
    var div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // --- Reorder helpers ---
  function moveMonitor(id, dir) {
    var ids = monitors.map(function(m) { return m.id; });
    var idx = ids.indexOf(id);
    if (idx < 0) return;
    var newIdx = idx + dir;
    if (newIdx < 0 || newIdx >= ids.length) return;
    var tmp = ids[idx];
    ids[idx] = ids[newIdx];
    ids[newIdx] = tmp;
    fetch('/api/monitors/reorder', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ids: ids}),
      credentials: 'same-origin'
    }).then(function(r) { return r.json(); }).then(function(d) {
      if (d.ok) refreshList();
    });
  }

  function moveGroup(id, dir) {
    var ids = lastGroupOrder.slice();
    var idx = ids.indexOf(id);
    if (idx < 0) return;
    var newIdx = idx + dir;
    if (newIdx < 0 || newIdx >= ids.length) return;
    var tmp = ids[idx];
    ids[idx] = ids[newIdx];
    ids[newIdx] = tmp;
    fetch('/api/groups/reorder', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ids: ids}),
      credentials: 'same-origin'
    }).then(function(r) { return r.json(); }).then(function(d) {
      if (d.ok) refreshList();
    });
  }

  // --- Monitor Detail ---
  function selectMonitor(id) {
    selectedMonitorId = id;
    // Persist selection in sessionStorage (survives language switch page reload)
    try { sessionStorage.setItem('wink_selected', id); } catch(e) {}

    // Update selected state in list
    var items = document.querySelectorAll('#monitor-list .monitor-item');
    for (var i = 0; i < items.length; i++) {
      if (items[i].getAttribute('data-id') === id) {
        items[i].classList.add('monitor-item--selected');
      } else {
        items[i].classList.remove('monitor-item--selected');
      }
    }

    // On mobile: hide list, show detail
    var listPanel = document.getElementById('list-panel');
    var detailPanel = document.getElementById('detail-panel');
    if (window.innerWidth < 1024) {
      listPanel.classList.add('hidden');
      detailPanel.classList.remove('hidden');
      detailPanel.classList.add('flex');
    }

    // Show detail content, hide empty state
    document.getElementById('detail-empty').classList.add('hidden');
    document.getElementById('detail-content').classList.remove('hidden');
    document.getElementById('detail-content').classList.add('flex', 'flex-col');

    refreshDetail();
    startDetailPoll();
  }

  function refreshDetail() {
    if (!selectedMonitorId) return;

    var detailHeartbeat = document.getElementById('detail-heartbeat');
    var barCount = detailHeartbeat ? calcBarCount(detailHeartbeat) : 60;

    fetchJSON('/api/monitors/' + selectedMonitorId + '?points=' + barCount, function (err, data) {
      if (err || !data) return;

      // Status dot
      var dotEl = document.getElementById('detail-status-dot');
      dotEl.className = 'w-3 h-3 rounded-full';
      if (!data.enabled) {
        dotEl.classList.add('bg-gray-400');
      } else if (data.has_history) {
        dotEl.classList.add(data.is_up ? 'bg-green-500' : 'bg-red-500');
        if (!data.is_up) dotEl.classList.add('status-dot--down');
      } else {
        dotEl.classList.add('bg-gray-400');
      }

      // Name & meta
      document.getElementById('detail-name').textContent = data.name;
      document.getElementById('detail-meta').textContent = data.type.toUpperCase() + ' \u00b7 ' + data.target;

      // Toggle pause/resume button
      var toggleBtn = document.getElementById('detail-toggle');
      toggleBtn.textContent = data.enabled ? t('dash.pause') : t('dash.resume');
      toggleBtn.onclick = function () {
        fetch('/api/monitors/' + data.id + '/toggle', { method: 'POST', credentials: 'same-origin' })
          .then(function (res) { return res.json(); })
          .then(function () {
            refreshList();
            refreshDetail();
          });
      };

      // Edit, clone & delete
      document.getElementById('detail-edit').href = '/monitors/' + data.id + '/edit';
      document.getElementById('detail-clone').href = '/monitors/' + data.id + '/clone';
      document.getElementById('detail-delete-id').value = data.id;
      document.getElementById('detail-delete-form').onsubmit = function () {
        return confirm(t('dash.delete_confirm'));
      };

      // Uptime badges
      setUptimeEl('detail-uptime-24h', data.uptime_24h);
      setUptimeEl('detail-uptime-7d', data.uptime_7d);
      setUptimeEl('detail-uptime-30d', data.uptime_30d);

      // Response time
      document.getElementById('detail-response-time').textContent =
        data.response_time > 0 ? data.response_time + 'ms' : '-';

      // Last check
      document.getElementById('detail-last-check').textContent = timeAgo(data.last_check);

      // Type & interval
      document.getElementById('detail-type').textContent = data.type.toUpperCase();
      document.getElementById('detail-interval').textContent = data.interval + 's';

      // Heartbeat bars
      if (detailHeartbeat) {
        renderBars(detailHeartbeat, data.heartbeats || [], barCount);
      }

      // Incidents
      renderIncidents(data.incidents || []);
    });
  }

  function setUptimeEl(elId, val) {
    var el = document.getElementById(elId);
    if (!el) return;
    el.textContent = val.toFixed(2) + '%';
    el.className = 'text-2xl font-semibold ' + uptimeClass(val);
  }

  function renderIncidents(incidents) {
    var container = document.getElementById('detail-incidents');
    if (!container) return;

    if (incidents.length === 0) {
      container.innerHTML = '<p class="text-sm text-gray-400">' + t('dash.no_incidents') + '</p>';
      return;
    }

    var html = '<div class="space-y-2">';
    // Show most recent first
    for (var i = incidents.length - 1; i >= 0; i--) {
      var inc = incidents[i];
      var isOpen = !inc.resolved_at;
      var statusColor = isOpen
        ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 border-red-200 dark:border-red-800'
        : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-700';

      html += '<div class="border rounded px-3 py-2 text-sm ' + statusColor + '">';
      html += '<div class="flex items-center justify-between">';
      html += '<span>' + (isOpen ? t('dash.status_down') + ' - ' + t('dash.ongoing') : t('dash.status_down')) + '</span>';
      html += '<span class="text-xs">' + new Date(inc.started_at * 1000).toLocaleString() + '</span>';
      html += '</div>';
      if (inc.reason) {
        html += '<div class="text-xs mt-1 opacity-75">' + escapeHtml(inc.reason) + '</div>';
      }
      if (!isOpen && inc.duration) {
        html += '<div class="text-xs mt-1">' + t('dash.duration') + ' ' + formatDuration(inc.duration) + '</div>';
      }
      html += '</div>';
    }
    html += '</div>';
    container.innerHTML = html;
  }

  function showList() {
    var listPanel = document.getElementById('list-panel');
    var detailPanel = document.getElementById('detail-panel');
    listPanel.classList.remove('hidden');
    if (window.innerWidth < 1024) {
      detailPanel.classList.add('hidden');
      detailPanel.classList.remove('flex');
    }
  }

  // --- Polling ---
  function startListPoll() {
    stopListPoll();
    listPollTimer = setInterval(function () {
      if (isPageVisible) refreshList();
    }, POLL_INTERVAL);
  }

  function stopListPoll() {
    if (listPollTimer) { clearInterval(listPollTimer); listPollTimer = null; }
  }

  function startDetailPoll() {
    stopDetailPoll();
    detailPollTimer = setInterval(function () {
      if (isPageVisible && selectedMonitorId) refreshDetail();
    }, POLL_INTERVAL);
  }

  function stopDetailPoll() {
    if (detailPollTimer) { clearInterval(detailPollTimer); detailPollTimer = null; }
  }

  // --- Resize handler ---
  var resizeTimer = null;
  function onResize() {
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () {
      refreshList();
      if (selectedMonitorId) refreshDetail();

      // Fix panel visibility on resize across breakpoint
      var listPanel = document.getElementById('list-panel');
      var detailPanel = document.getElementById('detail-panel');
      if (!listPanel || !detailPanel) return;

      if (window.innerWidth >= 1024) {
        // Desktop: both visible
        listPanel.classList.remove('hidden');
        detailPanel.classList.remove('hidden');
        detailPanel.classList.add('flex');
      }
    }, 250);
  }

  // --- Visibility ---
  function onVisibilityChange() {
    isPageVisible = !document.hidden;
    if (isPageVisible) {
      refreshList();
      if (selectedMonitorId) refreshDetail();
    }
  }

  // --- Init ---
  function init() {
    // Theme toggle
    var themeBtn = document.getElementById('theme-toggle');
    if (themeBtn) {
      themeBtn.addEventListener('click', toggleTheme);
    }

    // Back button (mobile detail -> list)
    var backBtn = document.getElementById('detail-back');
    if (backBtn) {
      backBtn.addEventListener('click', function () {
        showList();
      });
    }

    // If we're on the dashboard page, start polling
    var dashboard = document.getElementById('dashboard');
    if (dashboard) {
      // Restore selected monitor from sessionStorage (survives language switch)
      var savedId = '';
      try { savedId = sessionStorage.getItem('wink_selected') || ''; } catch(e) {}
      if (savedId) {
        selectedMonitorId = savedId;
        // Show detail panel immediately so it's ready when data arrives
        document.getElementById('detail-empty').classList.add('hidden');
        document.getElementById('detail-content').classList.remove('hidden');
        document.getElementById('detail-content').classList.add('flex', 'flex-col');
        refreshDetail();
        startDetailPoll();
      }

      refreshList();
      startListPoll();

      window.addEventListener('resize', onResize);
      document.addEventListener('visibilitychange', onVisibilityChange);
    }
  }

  // Run on DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
