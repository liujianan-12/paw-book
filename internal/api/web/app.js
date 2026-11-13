// 公共前端：会话、请求、渲染工具。cookie 受限时回退 Authorization: Bearer。
const PB = (() => {
  const tokenKey = 'pb_token';
  const labels = {
    PENDING_PAYMENT: '待支付', CONFIRMED: '已确认', CANCELLATION_REVIEW: '取消审核中',
    CHECKED_IN: '已到店', IN_SERVICE: '服务中', COMPLETED: '已完成',
    CANCELLED: '已取消', EXPIRED: '已过期', NO_SHOW: '未到店',
    PENDING: '待处理', PROCESSING: '退款中', SUCCESS: '已完成', FAILED: '失败',
    APPROVED: '已批准', REJECTED: '已拒绝', AUTO_ACCEPTED: '自动受理',
    CONFIRMED_PAY: '已确认', DUPLICATE_IGNORED: '重复忽略', ANOMALY_REFUND: '异常退款',
  };
  const tones = {
    COMPLETED: 'ok', APPROVED: 'ok', SUCCESS: 'ok', CONFIRMED: 'info', CHECKED_IN: 'info',
    IN_SERVICE: 'info', PENDING: 'warn', CANCELLATION_REVIEW: 'warn', PROCESSING: 'warn',
    PENDING_PAYMENT: 'warn', FAILED: 'bad', CANCELLED: 'bad', EXPIRED: 'bad', NO_SHOW: 'bad', REJECTED: 'bad',
  };
  function token() { return sessionStorage.getItem(tokenKey) || ''; }
  function setToken(v) { v ? sessionStorage.setItem(tokenKey, v) : sessionStorage.removeItem(tokenKey); }

  async function raw(method, path, body) {
    const headers = { 'Content-Type': 'application/json' };
    if (token()) headers.Authorization = 'Bearer ' + token();
    const r = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
    const j = await r.json().catch(() => ({ code: 'BAD_RESPONSE', message: '响应不是 JSON' }));
    return { status: r.status, json: j };
  }
  async function api(method, path, body) {
    const { status, json } = await raw(method, path, body);
    if (status === 401) {
      setToken('');
      location.href = '/login.html?next=' + encodeURIComponent(location.pathname);
      throw new Error('UNAUTHENTICATED: 未登录');
    }
    if (json.code !== 0 && json.code !== undefined) throw new Error(json.code + ': ' + json.message);
    return json.data;
  }
  async function requireLogin(role) {
    const me = await api('GET', '/api/me');
    if (role && me.Role !== role) {
      toast('当前账号角色为 ' + me.Role + '，请用 ' + role + ' 账号登录', true);
      location.href = '/login.html?next=' + encodeURIComponent(location.pathname);
      return null;
    }
    const who = document.querySelector('[data-who]');
    if (who) who.textContent = me.Name + '（' + me.Role + '）';
    return me;
  }
  function badge(s) {
    if (!s) return '<span class="muted">-</span>';
    const tone = tones[s] || '';
    return '<span class="badge ' + tone + '">' + (labels[s] || s) + '</span>';
  }
  function money(c) { return '<span class="money">¥' + (Number(c || 0) / 100).toFixed(2) + '</span>'; }
  function esc(v) {
    return String(v == null ? '' : v).replace(/[&<>"']/g, m => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m]));
  }
  function empty(tbody, cols, text) {
    tbody.innerHTML = '<tr><td class="empty" colspan="' + cols + '">' + esc(text || '暂无数据') + '</td></tr>';
  }
  function toast(msg, isErr) {
    let box = document.getElementById('toast');
    if (!box) { box = document.createElement('div'); box.id = 'toast'; document.body.appendChild(box); }
    const d = document.createElement('div');
    if (isErr) d.className = 'err';
    d.textContent = msg;
    box.appendChild(d);
    setTimeout(() => d.remove(), 4200);
  }
  function logout() { api('POST', '/api/auth/logout').catch(() => {}); setToken(''); location.href = '/login.html'; }
  return { api, raw, requireLogin, badge, money, esc, empty, toast, token, setToken, logout };
})();
