-- 002_seed.sql  预置数据（可重复执行；时段由 store.SeedSlots 按当前时间生成）
-- 演示账号密码统一为 123456，存 SHA-256（非明文）；账号仅用于教学演示。

INSERT OR IGNORE INTO store(id, name) VALUES (1, '宠物护理门店');

INSERT OR IGNORE INTO staff(id, store_id, name, role, account, password_hash) VALUES
 (1, 1, '店长张', 'manager',   'mgr', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92'),
 (2, 1, '前台李', 'reception', 'rec', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92'),
 (3, 1, '服务王', 'server',    'srv', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92'),
 (4, 1, '客服赵', 'cs',        'cs1', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92');

INSERT OR IGNORE INTO service_item(id, store_id, name, price_cents, active) VALUES (1, 1, '洗护', 9900, 1);

INSERT OR IGNORE INTO app_user(id, name, phone, account, password_hash) VALUES
 (1, '张三', '13800000001', 'u1', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92'),
 (2, '李四', '13800000002', 'u2', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92'),
 (3, '王五', '13800000003', 'u3', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92');

INSERT OR IGNORE INTO pet(id, user_id, name, breed) VALUES
 (1, 1, '豆豆', '金毛'),
 (2, 1, '咪咪', '布偶'),
 (3, 2, '乐乐', '柯基');
