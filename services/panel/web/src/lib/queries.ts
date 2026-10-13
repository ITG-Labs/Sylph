import { useQuery, useQueryClient } from "@tanstack/react-query"
import { core } from "./api"
import type { Group, Plan } from "./types"

export function usePlans() {
  return useQuery({ queryKey: ["plans"], queryFn: () => core.get<Plan[]>("/v1/admin/plans"), staleTime: 60_000 })
}

export function useGroups() {
  return useQuery({ queryKey: ["groups"], queryFn: () => core.get<Group[]>("/v1/admin/groups"), staleTime: 60_000 })
}

export function useInvalidate() {
  const qc = useQueryClient()
  return (...keys: unknown[][]) => Promise.all(keys.map((k) => qc.invalidateQueries({ queryKey: k })))
}
