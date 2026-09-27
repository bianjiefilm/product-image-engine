"use client";

import { useEffect, useId, useState } from "react";
import {
  CURRENT_APP,
  USEFUL_IMAGE_MATURE,
  type PayerScope,
} from "@/lib/eco-nav";
import styles from "./eco-nav.module.css";

export type NavAppLink = {
  appId: string;
  display: string;
  href: string | null;
  current: boolean;
};

export default function EcoTopNav({
  scope,
  badgeText,
  historyVisible,
  returnTo,
  switchable,
  apps,
  onSwitchScope,
  onLogout,
}: {
  scope: PayerScope;
  badgeText: string;
  historyVisible: boolean;
  returnTo: { label: string; href: string; title: string } | null;
  switchable: PayerScope[];
  apps: NavAppLink[];
  onSwitchScope: (scope: PayerScope) => void;
  onLogout: () => void;
}) {
  const [open, setOpen] = useState<"apps" | "user" | "scope" | null>(null);
  const appsId = useId();

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(null);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <header
      className={styles.bar}
      aria-label="生态导航"
      data-eco-topnav="true"
      data-history-visible={historyVisible ? "true" : "false"}
      data-useful-mature={USEFUL_IMAGE_MATURE ? "true" : "false"}
      data-app={CURRENT_APP.appId}
    >
      <span className={styles.appName} aria-current="page">
        {CURRENT_APP.display}
      </span>
      <span className={`${styles.srOnly}`}>当前应用</span>

      <div className={styles.wrap}>
        <button
          type="button"
          className={styles.menuBtn}
          aria-expanded={open === "apps"}
          aria-controls={appsId}
          onClick={() => setOpen((v) => (v === "apps" ? null : "apps"))}
        >
          应用
        </button>
        {open === "apps" ? (
          <div id={appsId} className={styles.panel}>
            <ul>
              {apps.map((app) => (
                <li key={app.appId}>
                  {app.href && !app.current ? (
                    <a
                      href={app.href}
                      data-intent="manual_switch"
                      rel="noreferrer"
                      referrerPolicy="no-referrer"
                    >
                      {app.display}
                    </a>
                  ) : (
                    <span aria-current={app.current ? "page" : undefined}>
                      {app.display}
                      {app.current ? "（当前）" : ""}
                    </span>
                  )}
                </li>
              ))}
            </ul>
            <p className={styles.note}>
              切换应用不会带走当前商品、提示词或工程，也不会新建交接。
            </p>
          </div>
        ) : null}
      </div>

      {returnTo ? (
        <a
          className={styles.chip}
          href={returnTo.href}
          title={returnTo.title}
          rel="noreferrer"
          referrerPolicy="no-referrer"
        >
          返回 {returnTo.label}
        </a>
      ) : null}

      <div className={styles.spacer} />

      <div className={styles.wrap}>
        <button
          type="button"
          className={styles.switchBtn}
          aria-expanded={open === "scope"}
          onClick={() => setOpen((v) => (v === "scope" ? null : "scope"))}
        >
          {scope.display}
        </button>
        {open === "scope" ? (
          <div className={styles.panel}>
            <p className={styles.note}>当前：{scope.display}</p>
            {switchable.length === 0 ? (
              <p className={styles.note}>没有其他已验证的组织可切换。</p>
            ) : (
              switchable.map((item) => (
                <button
                  key={`${item.kind}:${item.tenantId ?? ""}`}
                  type="button"
                  className={styles.switchBtn}
                  onClick={() => {
                    onSwitchScope(item);
                    setOpen(null);
                  }}
                >
                  {item.display}
                </button>
              ))
            )}
          </div>
        ) : null}
      </div>

      <span className={styles.badge} title="人民币或渠道额度">
        {badgeText}
      </span>

      <div className={styles.wrap}>
        <button
          type="button"
          className={styles.menuBtn}
          aria-expanded={open === "user"}
          onClick={() => setOpen((v) => (v === "user" ? null : "user"))}
        >
          账号
        </button>
        {open === "user" ? (
          <div className={styles.panel}>
            <button type="button" onClick={onLogout}>
              退出登录
            </button>
            <p className={styles.note}>
              顶栏不代表主体保真或背景生成已经通过。
            </p>
          </div>
        ) : null}
      </div>
    </header>
  );
}
