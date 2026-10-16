import { useQuery, useQueryClient } from "@tanstack/react-query"
import { core } from "./api"
import { normalizeDoc, type GeoSource, type Profile } from "./routing"
import type { Group, Node, Plan } from "./types"

export function usePlans() {
  return useQuery({ queryKey: ["plans"], queryFn: () => core.get<Plan[]>("/v1/admin/plans"), staleTime: 60_000 })
}

export function useGroups() {
  return useQuery({ queryKey: ["groups"], queryFn: () => core.get<Group[]>("/v1/admin/groups"), staleTime: 60_000 })
}

export function useNodes() {
  return useQuery({ queryKey: ["nodes"], queryFn: () => core.get<Node[]>("/v1/admin/nodes"), staleTime: 15_000 })
}

export function useProfiles() {
  return useQuery({
    queryKey: ["profiles"],
    queryFn: async () => (await core.get<Profile[]>("/v1/admin/profiles")).map((p) => ({ ...p, doc: normalizeDoc(p.doc) })),
    staleTime: 15_000,
  })
}

export function useGeoSources() {
  return useQuery({ queryKey: ["geo-sources"], queryFn: () => core.get<GeoSource[]>("/v1/admin/geo-sources"), staleTime: 60_000 })
}

export function useInvalidate() {
  const qc = useQueryClient()
  return (...keys: unknown[][]) => Promise.all(keys.map((k) => qc.invalidateQueries({ queryKey: k })))
}
