import type {
  ProxyCheckResult,
  ProxyDeleteResult,
  ProxyGlobalRebalanceResult,
  ProxyImportResult,
  ProxyNode,
  ProxyRebalanceResult,
} from "@/types/models";
import http from "@/utils/http";

export const proxiesApi = {
  async list(): Promise<ProxyNode[]> {
    const res = await http.get("/proxies");
    return res.data || [];
  },

  async import(proxiesText: string): Promise<ProxyImportResult> {
    const res = await http.post("/proxies/import", { proxies_text: proxiesText });
    return res.data;
  },

  async check(proxyIds: number[] = []): Promise<ProxyCheckResult> {
    const res = await http.post("/proxies/check", { proxy_ids: proxyIds });
    return res.data;
  },

  async rebalance(groupId: number, proxyIds: number[]): Promise<ProxyRebalanceResult> {
    const res = await http.post("/proxies/rebalance", {
      group_id: groupId,
      proxy_ids: proxyIds,
    });
    return res.data;
  },

  async rebalanceAllHealthy(): Promise<ProxyGlobalRebalanceResult> {
    const res = await http.post("/proxies/rebalance-all");
    return res.data;
  },

  async delete(proxyId: number): Promise<ProxyDeleteResult> {
    const res = await http.delete(`/proxies/${proxyId}`);
    return res.data;
  },
};
