import { useQuery } from "@tanstack/react-query"
import { auth } from "./api"
import { ApiError } from "./errors"

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await auth.me()
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null
        throw e
      }
    },
    staleTime: 60_000,
  })
}
