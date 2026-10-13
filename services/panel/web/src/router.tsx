import { createBrowserRouter } from "react-router"
import { AppShell } from "./components/layout/AppShell"
import { LoginPage } from "./pages/LoginPage"
import { OverviewPage } from "./pages/OverviewPage"
import { PlaceholderPage } from "./pages/PlaceholderPage"

export const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  {
    path: "/",
    element: <AppShell />,
    children: [
      { index: true, element: <OverviewPage /> },
      { path: "users", element: <PlaceholderPage title="Юзеры" /> },
      { path: "users/:id", element: <PlaceholderPage title="Юзер" /> },
      { path: "payments", element: <PlaceholderPage title="Платежи" /> },
      { path: "nodes", element: <PlaceholderPage title="Ноды" /> },
      { path: "plans", element: <PlaceholderPage title="Тарифы" /> },
      { path: "groups", element: <PlaceholderPage title="Группы" /> },
      { path: "audit", element: <PlaceholderPage title="Аудит" /> },
      { path: "*", element: <PlaceholderPage title="Нет такой страницы" /> },
    ],
  },
])
