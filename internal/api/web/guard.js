// 页面鉴权守卫：未登录跳登录页；角色不符给出提示并跳登录页。
async function requireLogin(expectRole) {
  const r = await fetch('/api/me');
  if (r.status === 401) {
    location.href = '/login.html?next=' + encodeURIComponent(location.pathname);
    return null;
  }
  const j = await r.json();
  if (j.code !== 0) {
    location.href = '/login.html';
    return null;
  }
  if (expectRole && j.data.Role !== expectRole) {
    alert('当前账号角色为 ' + j.data.Role + '，请使用 ' + expectRole + ' 账号登录');
    location.href = '/login.html?next=' + encodeURIComponent(location.pathname);
    return null;
  }
  return j.data;
}
