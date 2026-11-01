import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "react-router"
import { MotionConfig } from "motion/react"
import { Toaster, toast } from "sonner"
import { router } from "./router"
import { Backdrop } from "./components/layout/Backdrop"
import { ApiError } from "./lib/errors"
import { createQueryErrorReporter } from "./lib/queryErrors"
import "./index.css"

const reporter = createQueryErrorReporter((t) => toast.error(t.message, { id: t.id }))

const queryCache = new QueryCache({
  onError: (err, query) => reporter.onError(err, query),
  onSuccess: (_data, query) => reporter.onSuccess(query),
})
queryCache.subscribe((event) => {
  if (event.type === "removed") reporter.forget(event.query)
})

const queryClient = new QueryClient({
  queryCache,
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: true,
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
    },
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <MotionConfig reducedMotion="user">
        <Backdrop />
        <RouterProvider router={router} />
        <Toaster theme="dark" position="top-center" richColors closeButton />
      </MotionConfig>
    </QueryClientProvider>
  </StrictMode>,
)
