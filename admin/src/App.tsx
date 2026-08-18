import {
  AuditOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  FlagOutlined,
  LogoutOutlined,
  SafetyOutlined,
  SettingOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import {
  Alert,
  Button,
  Card,
  Col,
  Form,
  Input,
  Layout,
  Menu,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from "antd";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  api,
  APIError,
  type Loadout,
  type Permission,
  type Role,
  setCSRF,
  type User,
} from "./api";
import { LoadoutEditor } from "./LoadoutEditor";

const { Header, Content, Sider } = Layout;
const passwordPattern = /^(?=.*[A-Za-z])(?=.*\d)(?=.*[!@#$%^&*_+=-])[A-Za-z\d!@#$%^&*_+=-]{8,16}$/;
const passwordHelp = "8～16 位，须包含英文字母、数字和特殊符号；支持 ! @ # $ % ^ & * _ - + =";
type Page =
  | "overview"
  | "users"
  | "roles"
  | "loadouts"
  | "reports"
  | "audit"
  | "settings";
const pageFromHash = (): Page => {
  const value = location.hash.replace(/^#\/?/, "") as Page;
  return [
    "overview",
    "users",
    "roles",
    "loadouts",
    "reports",
    "audit",
    "settings",
  ].includes(value)
    ? value
    : "overview";
};
export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState<Page>(pageFromHash());
  useEffect(() => {
    void api<{ user: User }>("/auth/me")
      .then((value) => setUser(value.user))
      .catch(() => setUser(null))
      .finally(() => setLoading(false));
    const handler = () => setPage(pageFromHash());
    addEventListener("hashchange", handler);
    return () => removeEventListener("hashchange", handler);
  }, []);
  if (loading) return <div className="center-page">正在检查会话……</div>;
  if (!user) return <Login onLogin={setUser} />;
  if (user.must_change_password)
    return <ChangePassword username={user.username} />;
  if (!user.roles.includes("super_admin"))
    return (
      <div className="center-page">
        <Alert
          type="error"
          showIcon
          message="无权访问"
          description="当前账号不是超级管理员。"
        />
        <Button onClick={() => void logout()}>退出登录</Button>
      </div>
    );
  async function logout() {
    try {
      await api("/auth/logout", { method: "POST" });
    } finally {
      setCSRF("");
      location.reload();
    }
  }
  return (
    <Layout className="shell">
      <Sider width={224} theme="light" className="sider">
        <div className="brand">
          MHED <span>ADMIN</span>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[page]}
          onClick={({ key }) => {
            location.hash = `#/${key}`;
          }}
          items={[
            { key: "overview", icon: <DashboardOutlined />, label: "概览" },
            { key: "users", icon: <TeamOutlined />, label: "用户" },
            { key: "roles", icon: <SafetyOutlined />, label: "角色与权限" },
            { key: "loadouts", icon: <DatabaseOutlined />, label: "公开配装" },
            { key: "reports", icon: <FlagOutlined />, label: "举报处理" },
            { key: "audit", icon: <AuditOutlined />, label: "审计日志" },
            { key: "settings", icon: <SettingOutlined />, label: "系统设置" },
          ]}
        />
      </Sider>
      <Layout>
        <Header className="header">
          <Typography.Text strong>管理后台</Typography.Text>
          <Space>
            <Tag color="gold">测试环境</Tag>
            <Typography.Text>{user.username}</Typography.Text>
            <Button
              type="text"
              icon={<LogoutOutlined />}
              onClick={() => void logout()}
            >
              退出
            </Button>
          </Space>
        </Header>
        <Content className="content">
          {page === "overview" && <Overview />}
          {page === "users" && <UsersPage />}
          {page === "roles" && <RolesPage />}
          {page === "loadouts" && <LoadoutsPage />}
          {page === "reports" && <ReportsPage />}
          {page === "audit" && <AuditPage />}
          {page === "settings" && <EmailSettingsPage />}
        </Content>
      </Layout>
    </Layout>
  );
}

function Login({ onLogin }: { onLogin: (user: User) => void }) {
  const [submitting, setSubmitting] = useState(false);
  async function submit(values: { username: string; password: string }) {
    setSubmitting(true);
    try {
      const response = await api<{ user: User }>("/auth/login", {
        method: "POST",
        body: JSON.stringify(values),
      });
      onLogin(response.user);
    } catch (error) {
      showError(error);
    } finally {
      setSubmitting(false);
    }
  }
  return (
    <div className="login-page">
      <Card className="login-card">
        <div className="login-brand">
          MHED <span>ADMIN</span>
        </div>
        <Typography.Title level={3}>管理员登录</Typography.Title>
        <Typography.Paragraph type="secondary">
          使用服务器本地引导创建的超级管理员账号。
        </Typography.Paragraph>
        <Form layout="vertical" onFinish={(values) => void submit(values)}>
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true }]}
          >
            <Input autoFocus autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Button block type="primary" htmlType="submit" loading={submitting}>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  );
}
function ChangePassword({ username }: { username: string }) {
  const [submitting, setSubmitting] = useState(false);
  async function submit(values: {
    current: string;
    password: string;
    confirm: string;
  }) {
    if (values.password !== values.confirm) {
      message.error("两次输入的新密码不一致。");
      return;
    }
    setSubmitting(true);
    try {
      await api("/auth/change-password", {
        method: "POST",
        body: JSON.stringify({
          current_password: values.current,
          new_password: values.password,
        }),
      });
      setCSRF("");
      message.success("密码已修改，请重新登录。");
      location.reload();
    } catch (error) {
      showError(error);
    } finally {
      setSubmitting(false);
    }
  }
  return (
    <div className="login-page">
      <Card className="login-card">
        <Alert type="warning" showIcon message="首次登录必须修改临时密码" />
        <Typography.Title level={3}>{username}</Typography.Title>
        <Form layout="vertical" onFinish={(values) => void submit(values)}>
          <Form.Item
            name="current"
            label="当前临时密码"
            rules={[{ required: true }]}
          >
            <Input.Password />
          </Form.Item>
          <Form.Item
            name="password"
            label="新密码"
            extra={passwordHelp}
            rules={[
              { required: true },
              { pattern: passwordPattern, message: passwordHelp },
            ]}
          >
            <Input.Password />
          </Form.Item>
          <Form.Item
            name="confirm"
            label="确认新密码"
            rules={[{ required: true }]}
          >
            <Input.Password />
          </Form.Item>
          <Button block type="primary" htmlType="submit" loading={submitting}>
            修改密码
          </Button>
        </Form>
      </Card>
    </div>
  );
}
function PageHeader({ title, extra }: { title: string; extra?: ReactNode }) {
  return (
    <div className="page-header">
      <Typography.Title level={2}>{title}</Typography.Title>
      {extra}
    </div>
  );
}
function Overview() {
  const [data, setData] = useState({
    active_users: 0,
    published_loadouts: 0,
    open_reports: 0,
  });
  useEffect(() => {
    void api<typeof data>("/admin/dashboard").then(setData).catch(showError);
  }, []);
  return (
    <>
      <PageHeader title="平台概览" />
      <Row gutter={[16, 16]}>
        {[
          ["活跃用户", data.active_users],
          ["公开配装", data.published_loadouts],
          ["待处理举报", data.open_reports],
        ].map(([title, value]) => (
          <Col xs={24} md={8} key={title}>
            <Card>
              <Typography.Text type="secondary">{title}</Typography.Text>
              <div className="metric">{value}</div>
            </Card>
          </Col>
        ))}
      </Row>
    </>
  );
}

function UsersPage() {
  const [users, setUsers] = useState<User[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [createOpen, setCreateOpen] = useState(false);
  const [roleUser, setRoleUser] = useState<User | null>(null);
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const load = useCallback(() => {
    void Promise.all([
      api<{ items: User[] }>("/admin/users"),
      api<{ items: Role[] }>("/admin/roles"),
    ])
      .then(([u, r]) => {
        setUsers(u.items);
        setRoles(r.items);
      })
      .catch(showError);
  }, []);
  useEffect(load, [load]);
  async function create(values: { username: string; email?: string }) {
    try {
      const result = await api<{
        temporary_password: string;
        username: string;
      }>("/admin/users", { method: "POST", body: JSON.stringify(values) });
      setCreateOpen(false);
      Modal.info({
        title: "临时密码（仅显示一次）",
        width: 520,
        content: (
          <Input.TextArea
            readOnly
            rows={3}
            value={`username=${result.username}\npassword=${result.temporary_password}`}
          />
        ),
      });
      load();
    } catch (error) {
      showError(error);
    }
  }
  async function status(user: User, value: string) {
    try {
      await api(`/admin/users/${user.id}/status`, {
        method: "PATCH",
        body: JSON.stringify({ status: value }),
      });
      load();
    } catch (error) {
      showError(error);
    }
  }
  async function reset(user: User) {
    try {
      const result = await api<{ temporary_password: string }>(
        `/admin/users/${user.id}/reset-password`,
        { method: "POST", body: "{}" },
      );
      Modal.info({
        title: `${user.username} 的新临时密码`,
        content: <Input.TextArea readOnly value={result.temporary_password} />,
      });
      load();
    } catch (error) {
      showError(error);
    }
  }
  async function saveRoles() {
    if (!roleUser) return;
    try {
      await api(`/admin/users/${roleUser.id}/roles`, {
        method: "PUT",
        body: JSON.stringify({ role_ids: selectedRoles }),
      });
      setRoleUser(null);
      load();
    } catch (error) {
      showError(error);
    }
  }
  return (
    <>
      <PageHeader
        title="用户管理"
        extra={
          <Button type="primary" onClick={() => setCreateOpen(true)}>
            创建用户
          </Button>
        }
      />
      <Card>
        <Table
          rowKey="id"
          dataSource={users}
          columns={[
            {
              title: "公开身份",
              render: (_, row) => `${row.nickname} (#${row.public_id})`,
            },
            { title: "登录用户名", dataIndex: "username" },
            {
              title: "邮箱",
              render: (_, row) =>
                row.email ? (
                  <Space size={4}>
                    {row.email}
                    <Tag color={row.email_verified ? "green" : "default"}>
                      {row.email_verified ? "已验证" : "未验证"}
                    </Tag>
                  </Space>
                ) : (
                  "—"
                ),
            },
            {
              title: "状态",
              dataIndex: "status",
              render: (value) => (
                <Tag color={value === "active" ? "green" : "red"}>{value}</Tag>
              ),
            },
            {
              title: "角色",
              dataIndex: "roles",
              render: (values: string[]) => (
                <Space wrap>
                  {values.length ? (
                    values.map((value) => <Tag key={value}>{value}</Tag>)
                  ) : (
                    <Typography.Text type="secondary">无角色</Typography.Text>
                  )}
                </Space>
              ),
            },
            {
              title: "强制改密",
              dataIndex: "must_change_password",
              render: (value) => (value ? "是" : "否"),
            },
            {
              title: "操作",
              render: (_, row) => (
                <Space>
                  <Button
                    size="small"
                    onClick={() => {
                      setRoleUser(row);
                      setSelectedRoles(
                        roles
                          .filter((role) => row.roles.includes(role.key))
                          .map((role) => role.id),
                      );
                    }}
                  >
                    角色
                  </Button>
                  <Button size="small" onClick={() => void reset(row)}>
                    重置密码
                  </Button>
                  {row.status === "active" ? (
                    <Popconfirm
                      title="确认禁用该用户？"
                      onConfirm={() => void status(row, "disabled")}
                    >
                      <Button size="small" danger>
                        禁用
                      </Button>
                    </Popconfirm>
                  ) : (
                    <Button
                      size="small"
                      onClick={() => void status(row, "active")}
                    >
                      恢复
                    </Button>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal
        title="创建用户"
        open={createOpen}
        footer={null}
        onCancel={() => setCreateOpen(false)}
      >
        <Form layout="vertical" onFinish={(values) => void create(values)}>
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, pattern: /^[A-Za-z0-9_]{3,32}$/ }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="email" label="邮箱（可选）">
            <Input />
          </Form.Item>
          <Button type="primary" htmlType="submit">
            创建并生成临时密码
          </Button>
        </Form>
      </Modal>
      <Modal
        title={`分配角色：${roleUser?.username ?? ""}`}
        open={!!roleUser}
        onCancel={() => setRoleUser(null)}
        onOk={() => void saveRoles()}
      >
        <Select
          mode="multiple"
          style={{ width: "100%" }}
          value={selectedRoles}
          onChange={setSelectedRoles}
          options={roles.map((role) => ({
            value: role.id,
            label: `${role.name} (${role.key})`,
          }))}
        />
      </Modal>
    </>
  );
}

function RolesPage() {
  const [roles, setRoles] = useState<Role[]>([]);
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [editing, setEditing] = useState<Role | "new" | null>(null);
  const load = useCallback(
    () =>
      void Promise.all([
        api<{ items: Role[] }>("/admin/roles"),
        api<{ items: Permission[] }>("/admin/permissions"),
      ])
        .then(([r, p]) => {
          setRoles(r.items);
          setPermissions(p.items);
        })
        .catch(showError),
    [],
  );
  useEffect(load, [load]);
  async function save(values: {
    key: string;
    name: string;
    description: string;
    permissions: string[];
  }) {
    try {
      if (editing === "new")
        await api("/admin/roles", {
          method: "POST",
          body: JSON.stringify(values),
        });
      else if (editing)
        await api(`/admin/roles/${editing.id}`, {
          method: "PUT",
          body: JSON.stringify(values),
        });
      setEditing(null);
      load();
    } catch (error) {
      showError(error);
    }
  }
  async function remove(role: Role) {
    try {
      await api(`/admin/roles/${role.id}`, { method: "DELETE" });
      load();
    } catch (error) {
      showError(error);
    }
  }
  return (
    <>
      <PageHeader
        title="角色与权限"
        extra={
          <Button type="primary" onClick={() => setEditing("new")}>
            新建角色
          </Button>
        }
      />
      <Card>
        <Table
          rowKey="id"
          dataSource={roles}
          columns={[
            {
              title: "角色",
              render: (_, row) => (
                <>
                  <Typography.Text strong>{row.name}</Typography.Text>
                  <div>
                    <Typography.Text type="secondary">
                      {row.key}
                    </Typography.Text>
                  </div>
                </>
              ),
            },
            {
              title: "权限",
              dataIndex: "permissions",
              render: (items: string[]) => (
                <Space wrap>
                  {items.map((item) => (
                    <Tag key={item}>{item}</Tag>
                  ))}
                </Space>
              ),
            },
            { title: "成员数", dataIndex: "member_count" },
            {
              title: "系统角色",
              dataIndex: "is_system",
              render: (value) => (value ? "是" : "否"),
            },
            {
              title: "操作",
              render: (_, row) => (
                <Space>
                  <Button size="small" onClick={() => setEditing(row)}>
                    编辑
                  </Button>
                  {!row.is_system && (
                    <Popconfirm
                      title="确认删除？"
                      onConfirm={() => void remove(row)}
                    >
                      <Button size="small" danger>
                        删除
                      </Button>
                    </Popconfirm>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal
        title={editing === "new" ? "新建角色" : "编辑角色"}
        open={!!editing}
        footer={null}
        destroyOnHidden
        onCancel={() => setEditing(null)}
      >
        {editing && (
          <Form
            layout="vertical"
            initialValues={editing === "new" ? { permissions: [] } : editing}
            onFinish={(values) => void save(values)}
          >
            <Form.Item name="key" label="标识" rules={[{ required: true }]}>
              <Input disabled={editing !== "new" && editing.is_system} />
            </Form.Item>
            <Form.Item name="name" label="名称" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="description" label="说明">
              <Input.TextArea />
            </Form.Item>
            <Form.Item name="permissions" label="权限">
              <Select
                mode="multiple"
                options={permissions.map((item) => ({
                  value: item.key,
                  label: `${item.name} (${item.key})`,
                }))}
              />
            </Form.Item>
            <Button type="primary" htmlType="submit">
              保存
            </Button>
          </Form>
        )}
      </Modal>
    </>
  );
}

function LoadoutsPage() {
  const [items, setItems] = useState<Loadout[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [editor, setEditor] = useState<Loadout | "new" | null>(null);
  const load = useCallback(
    () =>
      void Promise.all([
        api<{ items: Loadout[] }>("/admin/loadouts"),
        api<{ items: User[] }>("/admin/users"),
      ])
        .then(([l, u]) => {
          setItems(l.items);
          setUsers(u.items);
        })
        .catch(showError),
    [],
  );
  useEffect(load, [load]);
  async function edit(row: Loadout) {
    try {
      setEditor(await api<Loadout>(`/admin/loadouts/${row.id}`));
    } catch (error) {
      showError(error);
    }
  }
  async function status(row: Loadout, value: string) {
    try {
      await api(`/admin/loadouts/${row.id}/status`, {
        method: "PATCH",
        body: JSON.stringify({ status: value }),
      });
      load();
    } catch (error) {
      showError(error);
    }
  }
  if (editor)
    return (
      <LoadoutEditor
        users={users}
        initial={editor === "new" ? undefined : editor}
        onCancel={() => setEditor(null)}
        onDone={() => {
          setEditor(null);
          load();
        }}
      />
    );
  return (
    <>
      <PageHeader
        title="公开配装"
        extra={
          <Button type="primary" onClick={() => setEditor("new")}>
            可视化新建配装
          </Button>
        }
      />
      <Card>
        <Table
          rowKey="id"
          dataSource={items}
          columns={[
            { title: "名称", dataIndex: "name" },
            {
              title: "所有者",
              render: (_, row) => (
                <>
                  <div>
                    {row.owner_nickname} (#{row.owner_public_id})
                  </div>
                  <Typography.Text type="secondary">
                    {row.owner_username}
                  </Typography.Text>
                </>
              ),
            },
            {
              title: "状态",
              dataIndex: "status",
              render: (value) => (
                <Tag
                  color={
                    value === "published"
                      ? "green"
                      : value === "hidden"
                        ? "orange"
                        : "red"
                  }
                >
                  {value}
                </Tag>
              ),
            },
            {
              title: "合法性",
              dataIndex: "is_legal",
              render: (value) => (
                <Tag color={value ? "green" : "orange"}>
                  {value ? "合法" : "存在风险"}
                </Tag>
              ),
            },
            { title: "点赞", dataIndex: "like_count" },
            {
              title: "风险",
              dataIndex: "risk_summary",
              render: (value) => (
                <Tag color={value?.diagnostics?.length ? "orange" : "green"}>
                  {value?.diagnostics?.length ?? 0}
                </Tag>
              ),
            },
            { title: "版本", dataIndex: "version" },
            { title: "更新时间", dataIndex: "updated_at" },
            {
              title: "操作",
              render: (_, row) => (
                <Space>
                  <Button size="small" onClick={() => void edit(row)}>
                    编辑
                  </Button>
                  {row.status !== "published" && (
                    <Button
                      size="small"
                      onClick={() => void status(row, "published")}
                    >
                      恢复发布
                    </Button>
                  )}
                  {row.status === "published" && (
                    <Button
                      size="small"
                      onClick={() => void status(row, "hidden")}
                    >
                      隐藏
                    </Button>
                  )}
                  {row.status !== "deleted" && (
                    <Popconfirm
                      title="确认软删除？"
                      onConfirm={() => void status(row, "deleted")}
                    >
                      <Button size="small" danger>
                        删除
                      </Button>
                    </Popconfirm>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Card>
    </>
  );
}

type Report = {
  id: string;
  loadout_id: string;
  reason: string;
  details: string;
  evidence_name: string;
  evidence_remark: string;
  status: string;
  created_at: string;
  reporter_public_id?: number | null;
  reporter_nickname?: string | null;
};
function ReportsPage() {
  const [items, setItems] = useState<Report[]>([]);
  const load = useCallback(
    () =>
      void api<{ items: Report[] }>("/admin/reports")
        .then((value) => setItems(value.items))
        .catch(showError),
    [],
  );
  useEffect(load, [load]);
  async function resolve(row: Report, status: string, loadout_action: string) {
    try {
      await api(`/admin/reports/${row.id}/resolve`, {
        method: "POST",
        body: JSON.stringify({ status, note: "", loadout_action }),
      });
      load();
    } catch (error) {
      showError(error);
    }
  }
  return (
    <>
      <PageHeader title="举报处理" />
      <Card>
        <Table
          rowKey="id"
          dataSource={items}
          expandable={{
            expandedRowRender: (row) => (
              <Space direction="vertical">
                <Typography.Text>{row.details || "无详细说明"}</Typography.Text>
                <Typography.Text type="secondary">
                  证据备注：{row.evidence_remark || "—"}
                </Typography.Text>
              </Space>
            ),
          }}
          columns={[
            { title: "证据配装名", dataIndex: "evidence_name" },
            {
              title: "举报人",
              render: (_, row) =>
                row.reporter_nickname
                  ? `${row.reporter_nickname} (#${row.reporter_public_id})`
                  : "—",
            },
            { title: "原因", dataIndex: "reason" },
            { title: "状态", dataIndex: "status" },
            { title: "提交时间", dataIndex: "created_at" },
            {
              title: "操作",
              render: (_, row) =>
                row.status === "open" ? (
                  <Space>
                    <Button
                      size="small"
                      onClick={() => void resolve(row, "dismissed", "none")}
                    >
                      忽略
                    </Button>
                    <Button
                      size="small"
                      onClick={() => void resolve(row, "resolved", "hide")}
                    >
                      隐藏并结案
                    </Button>
                    <Button
                      size="small"
                      danger
                      onClick={() => void resolve(row, "resolved", "delete")}
                    >
                      删除并结案
                    </Button>
                  </Space>
                ) : (
                  "—"
                ),
            },
          ]}
        />
      </Card>
    </>
  );
}
type Audit = {
  id: number;
  action: string;
  target_type: string;
  target_id: string;
  request_id: string;
  created_at: string;
  actor_username?: string | null;
};
function AuditPage() {
  const [items, setItems] = useState<Audit[]>([]);
  useEffect(() => {
    void api<{ items: Audit[] }>("/admin/audit-logs")
      .then((value) => setItems(value.items))
      .catch(showError);
  }, []);
  return (
    <>
      <PageHeader title="审计日志" />
      <Card>
        <Table
          rowKey="id"
          dataSource={items}
          columns={[
            { title: "ID", dataIndex: "id" },
            {
              title: "操作人",
              dataIndex: "actor_username",
              render: (value) => value ?? "系统",
            },
            { title: "动作", dataIndex: "action" },
            {
              title: "目标",
              render: (_, row) => `${row.target_type} / ${row.target_id}`,
            },
            { title: "请求 ID", dataIndex: "request_id" },
            { title: "时间", dataIndex: "created_at" },
          ]}
        />
      </Card>
    </>
  );
}

type EmailSettings = {
  provider: string;
  enabled: boolean;
  has_api_key: boolean;
  template_id: string;
  sender_alias: string;
  reply_to: string;
  updated_at: string;
  send_api: string;
  balance_api: string;
  send_documentation: string;
  balance_documentation: string;
  template_variables: string[];
};
type EmailFeedback = { type: "success" | "error"; message: string };
type EmailDelivery = {
  id: number;
  purpose: "register" | "bind_email" | "reset_password" | "test";
  recipient: string;
  status: "queued" | "sending" | "sent" | "failed";
  attempt_count: number;
  provider_message_id: string;
  last_error_code: string;
  created_at: string;
  sent_at?: string | null;
};
function EmailSettingsPage() {
  const [form] = Form.useForm();
  const [settings, setSettings] = useState<EmailSettings | null>(null);
  const [saving, setSaving] = useState(false);
  const [checking, setChecking] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testEmail, setTestEmail] = useState("");
  const [feedback, setFeedback] = useState<EmailFeedback | null>(null);
  const [balanceResult, setBalanceResult] = useState<string | null>(null);
  const [testMessageId, setTestMessageId] = useState<string | null>(null);
  const [deliveries, setDeliveries] = useState<EmailDelivery[]>([]);
  const [deliveriesLoading, setDeliveriesLoading] = useState(false);
  const load = useCallback(
    () =>
      void api<EmailSettings>("/admin/settings/email")
        .then((value) => {
          setSettings(value);
          form.setFieldsValue({
            enabled: value.enabled,
            api_key: "",
            template_id: value.template_id,
            sender_alias: value.sender_alias,
            reply_to: value.reply_to,
          });
        })
        .catch((error) =>
          setFeedback({
            type: "error",
            message:
              error instanceof Error ? error.message : "邮件配置读取失败。",
          }),
        ),
    [form],
  );
  useEffect(load, [load]);
  const loadDeliveries = useCallback(async () => {
    setDeliveriesLoading(true);
    try {
      const value = await api<{ items: EmailDelivery[] }>(
        "/admin/settings/email/deliveries",
      );
      setDeliveries(value.items);
    } catch (error) {
      setFeedback({
        type: "error",
        message: error instanceof Error ? error.message : "发送记录读取失败。",
      });
    } finally {
      setDeliveriesLoading(false);
    }
  }, []);
  useEffect(() => {
    void loadDeliveries();
  }, [loadDeliveries]);
  async function save(values: {
    enabled: boolean;
    api_key?: string;
    template_id: string;
    sender_alias: string;
    reply_to?: string;
  }) {
    setSaving(true);
    setFeedback(null);
    try {
      const value = await api<EmailSettings>("/admin/settings/email", {
        method: "PUT",
        body: JSON.stringify({
          ...values,
          api_key: values.api_key ?? "",
          reply_to: values.reply_to ?? "",
        }),
      });
      setSettings(value);
      form.setFieldValue("api_key", "");
      setFeedback({ type: "success", message: "邮件配置已保存。" });
    } catch (error) {
      setFeedback({
        type: "error",
        message: error instanceof Error ? error.message : "邮件配置保存失败。",
      });
    } finally {
      setSaving(false);
    }
  }
  async function balance() {
    setChecking(true);
    setFeedback(null);
    try {
      const value = await api<{ balance: string }>(
        "/admin/settings/email/check-balance",
        { method: "POST", body: "{}" },
      );
      setBalanceResult(value.balance);
    } catch (error) {
      setFeedback({
        type: "error",
        message: error instanceof Error ? error.message : "余额查询失败。",
      });
    } finally {
      setChecking(false);
    }
  }
  async function sendTest() {
    if (!testEmail) {
      setFeedback({ type: "error", message: "请先填写测试收件邮箱。" });
      return;
    }
    setTesting(true);
    setFeedback(null);
    try {
      const value = await api<{ status: string; message_id: string }>(
        "/admin/settings/email/test",
        { method: "POST", body: JSON.stringify({ to: testEmail }) },
      );
      setTestMessageId(value.message_id || "AOKSend 未返回消息 ID");
      void loadDeliveries();
    } catch (error) {
      setFeedback({
        type: "error",
        message: error instanceof Error ? error.message : "测试邮件发送失败。",
      });
    } finally {
      setTesting(false);
    }
  }
  return (
    <>
      <PageHeader title="系统设置" />
      {feedback && (
        <Alert
          closable
          showIcon
          type={feedback.type}
          message={feedback.message}
          onClose={() => setFeedback(null)}
          style={{ marginBottom: 16 }}
        />
      )}
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={15}>
          <Card
            title="AOKSend 邮件服务"
            extra={
              settings?.enabled ? (
                <Tag color="green">已启用</Tag>
              ) : (
                <Tag>未启用</Tag>
              )
            }
          >
            <Alert
              showIcon
              type="info"
              message="密钥经 AES-256-GCM 加密存储"
              description="已保存的 API 密钥不会返回浏览器；密钥输入框留空即可保留原值。"
              style={{ marginBottom: 20 }}
            />
            <Form
              form={form}
              layout="vertical"
              onFinish={(values) => void save(values)}
              initialValues={{ enabled: false, sender_alias: "MHED" }}
            >
              <Form.Item
                name="enabled"
                label="启用邮箱注册与找回密码"
                valuePropName="checked"
              >
                <Switch />
              </Form.Item>
              <Form.Item
                name="api_key"
                label="API 密钥"
                extra={
                  settings?.has_api_key
                    ? "当前已配置；只在需要更换时填写。"
                    : "尚未配置 API 密钥。"
                }
              >
                <Input.Password
                  autoComplete="new-password"
                  placeholder={
                    settings?.has_api_key
                      ? "留空保留当前密钥"
                      : "填写 AOKSend app_key"
                  }
                />
              </Form.Item>
              <Form.Item
                name="template_id"
                label="验证码模板 ID"
                rules={[{ max: 80 }]}
              >
                <Input placeholder="AOKSend 模板 ID" />
              </Form.Item>
              <Row gutter={16}>
                <Col span={12}>
                  <Form.Item
                    name="sender_alias"
                    label="发件人名称"
                    rules={[{ required: true, max: 80 }]}
                  >
                    <Input />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item
                    name="reply_to"
                    label="回复地址（可选）"
                    rules={[{ type: "email" }]}
                  >
                    <Input />
                  </Form.Item>
                </Col>
              </Row>
              <Space>
                <Button type="primary" htmlType="submit" loading={saving}>
                  保存配置
                </Button>
                <Button
                  onClick={() => void balance()}
                  loading={checking}
                  disabled={!settings?.has_api_key}
                >
                  查询余额
                </Button>
              </Space>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card title="发送测试">
            <Typography.Paragraph type="secondary">
              使用正式模板发送测试验证码
              `123456`，用于确认域名、密钥和模板均可用。
            </Typography.Paragraph>
            <Space.Compact block>
              <Input
                value={testEmail}
                onChange={(event) => setTestEmail(event.target.value)}
                placeholder="测试收件邮箱"
              />
              <Button
                type="primary"
                loading={testing}
                onClick={() => void sendTest()}
              >
                发送
              </Button>
            </Space.Compact>
          </Card>
          <Card title="固定模板参数" style={{ marginTop: 16 }}>
            <Space wrap>
              {(
                settings?.template_variables ?? ["code", "username", "userinfo"]
              ).map((value) => (
                <Tag color="blue" key={value}>{`{{${value}}}`}</Tag>
              ))}
            </Space>
            <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
              接口地址固定为 AOKSend API v2，不能从后台修改。
            </Typography.Paragraph>
            <Space direction="vertical">
              <a
                href={
                  settings?.send_documentation ??
                  "https://www.aoksend.com/api.html"
                }
                target="_blank"
                rel="noreferrer"
              >
                邮件发送 API 文档
              </a>
              <a
                href={
                  settings?.balance_documentation ??
                  "https://www.aoksend.com/check_balance_api.html"
                }
                target="_blank"
                rel="noreferrer"
              >
                余额查询 API 文档
              </a>
            </Space>
          </Card>
        </Col>
        <Col span={24}>
          <Card
            title="最近发送记录"
            extra={
              <Button
                size="small"
                loading={deliveriesLoading}
                onClick={() => void loadDeliveries()}
              >
                刷新
              </Button>
            }
          >
            <Table
              size="small"
              rowKey="id"
              loading={deliveriesLoading}
              dataSource={deliveries}
              pagination={{ pageSize: 10, showSizeChanger: false }}
              locale={{ emptyText: "暂无邮件发送记录" }}
              columns={[
                {
                  title: "场景",
                  dataIndex: "purpose",
                  render: (value: EmailDelivery["purpose"]) =>
                    ({
                      register: "注册验证",
                      bind_email: "绑定邮箱",
                      reset_password: "重置密码",
                      test: "测试邮件",
                    })[value] ?? value,
                },
                { title: "收件地址", dataIndex: "recipient" },
                {
                  title: "状态",
                  dataIndex: "status",
                  render: (value: EmailDelivery["status"]) => (
                    <Tag
                      color={
                        value === "sent"
                          ? "green"
                          : value === "failed"
                            ? "red"
                            : "blue"
                      }
                    >
                      {value === "sent"
                        ? "已发送"
                        : value === "failed"
                          ? "失败"
                          : value === "sending"
                            ? "发送中"
                            : "排队中"}
                    </Tag>
                  ),
                },
                { title: "尝试次数", dataIndex: "attempt_count" },
                {
                  title: "AOKSend 消息 ID",
                  dataIndex: "provider_message_id",
                  render: (value: string) => value || "—",
                },
                {
                  title: "错误码",
                  dataIndex: "last_error_code",
                  render: (value: string) => value || "—",
                },
                {
                  title: "创建时间",
                  dataIndex: "created_at",
                  render: (value: string) => new Date(value).toLocaleString(),
                },
              ]}
            />
          </Card>
        </Col>
      </Row>
      <Modal
        title="AOKSend 账户余额"
        open={balanceResult !== null}
        onOk={() => setBalanceResult(null)}
        onCancel={() => setBalanceResult(null)}
        cancelButtonProps={{ style: { display: "none" } }}
      >
        <Typography.Title level={2} style={{ margin: 0 }}>
          {balanceResult}
        </Typography.Title>
        <Typography.Text type="secondary">
          封（以 AOKSend 返回单位为准）
        </Typography.Text>
      </Modal>
      <Modal
        title="测试邮件发送成功"
        open={testMessageId !== null}
        onOk={() => setTestMessageId(null)}
        onCancel={() => setTestMessageId(null)}
        cancelButtonProps={{ style: { display: "none" } }}
      >
        <Alert
          showIcon
          type="success"
          message="AOKSend 已接受测试邮件"
          description={`收件地址：${testEmail}`}
        />
        <Typography.Paragraph style={{ marginTop: 16, marginBottom: 4 }}>
          消息 ID
        </Typography.Paragraph>
        <Input readOnly value={testMessageId ?? ""} />
      </Modal>
    </>
  );
}
function showError(error: unknown) {
  if (error instanceof APIError || error instanceof Error)
    message.error(error.message);
  else message.error("请求失败");
}
