import { createBrowserRouter } from "react-router"
import { AppShell } from "./components/layout/AppShell"
import { ErrorPage } from "./pages/ErrorPage"
import { LoginPage } from "./pages/LoginPage"
import { OverviewPage } from "./pages/OverviewPage"
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
      { path: "payments", element: <PlaceholderPage title="Платежи" /> },
      { path: "nodes", element: <PlaceholderPage title="Ноды" /> },
      { path: "plans", element: <PlaceholderPage title="Тарифы" /> },
      { path: "groups", element: <PlaceholderPage title="Группы" /> },
      { path: "audit", element: <PlaceholderPage title="Аудит" /> },
      { path: "*", element: <PlaceholderPage title="Нет такой страницы" /> },
        ],
      },
    ],
  },
])
