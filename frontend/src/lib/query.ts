import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchOnWindowFocus: false,
      retry: (failureCount, err) => {
        const status = (err as { status?: number })?.status ?? 0;
        // 4xx não são retentáveis (4xx/validation); rede/5xx sim.
        if (status >= 400 && status < 500) return false;
        return failureCount < 2;
      },
    },
    mutations: {
      retry: false,
    },
  },
});