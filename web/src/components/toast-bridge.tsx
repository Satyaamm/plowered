"use client";

import { useEffect } from "react";
import {
  Toast,
  ToastBody,
  ToastTitle,
  Toaster,
  useToastController,
} from "@fluentui/react-components";
import { TOASTER_ID, registerToastDispatcher } from "@/lib/toast";

// ToastBridge renders the Fluent <Toaster> and wires its imperative
// dispatcher into the module-level helper exposed by lib/toast.ts. Mount
// it ONCE near the root of the app — Providers handles that. Anyone
// (React components, React Query MutationCache callbacks, plain async
// code) can then call `toast.success("…")` without holding a hook.
export function ToastBridge() {
  const { dispatchToast } = useToastController(TOASTER_ID);

  useEffect(() => {
    registerToastDispatcher(({ title, body, intent }) => {
      dispatchToast(
        <Toast>
          <ToastTitle>{title}</ToastTitle>
          {body ? <ToastBody>{body}</ToastBody> : null}
        </Toast>,
        { intent, timeout: 4500 },
      );
    });
    return () => registerToastDispatcher(null);
  }, [dispatchToast]);

  return <Toaster toasterId={TOASTER_ID} position="top-end" />;
}
