import { createBrowserRouter } from "react-router"
import { AppShell } from "./components/layout/AppShell"
import { AuditPage } from "./pages/AuditPage"
import { ErrorPage } from "./pages/ErrorPage"
import { LoginPage } from "./pages/LoginPage"
import { NodePage } from "./pages/NodePage"
import { NodesPage } from "./pages/NodesPage"
import { GroupsPage } from "./pages/GroupsPage"
import { OverviewPage } from "./pages/OverviewPage"
import { PlanPage } from "./pages/PlanPage"
import { PlansPage } from "./pages/PlansPage"
import { PaymentPage } from "./pages/PaymentPage"
import { PaymentsPage } from "./pages/PaymentsPage"
import { PlaceholderPage } from "./pages/PlaceholderPage"
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
      { path: "plans", element: <PlansPage /> },
      { path: "plans/:id", element: <PlanPage /> },
      { path: "groups", element: <GroupsPage /> },
      { path: "audit", element: <AuditPage /> },
      { path: "*", element: <PlaceholderPage title="Нет такой страницы" /> },
        ],
      },
    ],
  },
])
