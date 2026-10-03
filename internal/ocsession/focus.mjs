import { createRoot, createEffect } from "solid-js";
import { writeFileSync, renameSync, unlinkSync } from "node:fs";

const path = __RA2A_LEASE__;
const pid = __RA2A_PID__;

export default {
  id: "ra2a-attachment",
  tui: async (api) => {
    let closed = false;
    const remove = () => {
      for (const file of [path, path + ".next"]) {
        try { unlinkSync(file); } catch (error) {
          if (error.code !== "ENOENT") console.warn("ra2a_attachment_remove_failed", error.message);
        }
      }
    };
    const publish = () => {
      if (closed) return;
      const route = api.route.current;
      const sessionID = route.name === "session" ? route.params.sessionID : "";
      try {
        writeFileSync(path + ".next", JSON.stringify({ sessionID, pid, expires: Date.now() + 3000 }), { mode: 0o600 });
        renameSync(path + ".next", path);
      } catch (error) {
        remove();
        console.warn("ra2a_attachment_publish_failed", error.message);
      }
    };
    const dispose = createRoot((dispose) => {
      createEffect(publish);
      return dispose;
    });
    const timer = setInterval(publish, 1000);
    api.lifecycle.onDispose(() => {
      closed = true;
      clearInterval(timer);
      dispose();
      remove();
    });
  },
};
