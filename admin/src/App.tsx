import { DashboardOutlined, DatabaseOutlined, TeamOutlined } from "@ant-design/icons";
import { Card, Col, Layout, Menu, Row, Space, Tag, Typography } from "antd";

const { Header, Content, Sider } = Layout;

export function App() {
  return (
    <Layout className="shell">
      <Sider width={224} theme="light" className="sider">
        <div className="brand">MHED <span>ADMIN</span></div>
        <Menu
          mode="inline"
          defaultSelectedKeys={["overview"]}
          items={[
            { key: "overview", icon: <DashboardOutlined />, label: "概览" },
            { key: "users", icon: <TeamOutlined />, label: "用户" },
            { key: "loadouts", icon: <DatabaseOutlined />, label: "配装" },
          ]}
        />
      </Sider>
      <Layout>
        <Header className="header">
          <Typography.Text strong>管理后台</Typography.Text>
          <Tag color="gold">测试环境</Tag>
        </Header>
        <Content className="content">
          <Space direction="vertical" size={24} style={{ width: "100%" }}>
            <div>
              <Typography.Title level={2}>平台概览</Typography.Title>
              <Typography.Text type="secondary">账号、配装和审核功能将在 API 契约确认后接入。</Typography.Text>
            </div>
            <Row gutter={[16, 16]}>
              {[
                ["已验证用户", "—"],
                ["公开配装", "—"],
                ["今日点赞", "—"],
              ].map(([title, value]) => (
                <Col xs={24} md={8} key={title}>
                  <Card><Typography.Text type="secondary">{title}</Typography.Text><div className="metric">{value}</div></Card>
                </Col>
              ))}
            </Row>
          </Space>
        </Content>
      </Layout>
    </Layout>
  );
}
