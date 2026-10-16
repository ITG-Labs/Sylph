import { createBrowserRouter } from "react-router"
import { AppShell } from "./components/layout/AppShell"
import { AuditPage } from "./pages/AuditPage"
import { ErrorPage } from "./pages/ErrorPage"
import { LoginPage } from "./pages/LoginPage"
import { NodePage } from "./pages/NodePage"
import { NodesPage } from "./pages/NodesPage"
import { GroupPage } from "./pages/GroupPage"
import { GroupsPage } from "./pages/GroupsPage"
import { OverviewPage } from "./pages/OverviewPage"
import { PlanPage } from "./pages/PlanPage"
import { PlansPage } from "./pages/PlansPage"
import { PaymentPage } from "./pages/PaymentPage"
import { PaymentsPage } from "./pages/PaymentsPage"
import { PlaceholderPage } from "./pages/PlaceholderPage"
import { ProfilePage } from "./pages/ProfilePage"
import { RoutingPage } from "./pages/RoutingPage"
import { SettingsPage } from "./pages/SettingsPage"
import { UserPage } from "./pages/UserPage"
import { UsersPage } from "./pages/UsersPage"

export const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  {
    path: "/",
    element: <AppShell />,
    children: [
      {
        errorElement: <ErrorPage />,
        children: [
      { index: true, element: <OverviewPage /> },
      { path: "users", element: <UsersPage /> },
      { path: "users/:id", element: <UserPage /> },
      { path: "payments", element: <PaymentsPage /> },
      { path: "payments/:paymentId", element: <PaymentPage /> },
      { path: "nodes", element: <NodesPage /> },
      { path: "nodes/:id", element: <NodePage /> },
      { path: "routing", element: <RoutingPage /> },
      { path: "routing/:id", element: <ProfilePage /> },
      { path: "plans", element: <PlansPage /> },
      { path: "plans/:id", element: <PlanPage /> },
      { path: "groups", element: <GroupsPage /> },
      { path: "groups/:id", element: <GroupPage /> },
      { path: "audit", element: <AuditPage /> },
      { path: "settings", element: <SettingsPage /> },
      { path: "*", element: <PlaceholderPage title="Нет такой страницы" /> },
        ],
      },
    ],
  },
])
