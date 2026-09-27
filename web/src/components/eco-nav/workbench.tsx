"use client";

import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type WorkbenchFact = {
  sourceType: string;
  sourceLabel: string;
  sourceTenantId: string | null;
  returnHref: string | null;
  enterpriseAssets: boolean;
};

const WorkbenchContext = createContext<{
  fact: WorkbenchFact | null;
  setFact: (fact: WorkbenchFact | null) => void;
} | null>(null);

export function WorkbenchProvider({ children }: { children: ReactNode }) {
  const [fact, setFact] = useState<WorkbenchFact | null>(null);
  const value = useMemo(() => ({ fact, setFact }), [fact]);
  return <WorkbenchContext.Provider value={value}>{children}</WorkbenchContext.Provider>;
}

export function useWorkbenchFact(): WorkbenchFact | null {
  return useContext(WorkbenchContext)?.fact ?? null;
}

export function usePublishWorkbench(fact: WorkbenchFact | null) {
  const setFact = useContext(WorkbenchContext)?.setFact;
  const key = JSON.stringify(fact);
  useEffect(() => {
    if (!setFact) return;
    setFact(fact);
    return () => setFact(null);
    // key 覆盖 fact 的字段变化；setFact 来自 useState，引用稳定。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [setFact, key]);
}
