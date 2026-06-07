import { joinSession } from "@github/copilot-sdk/extension";
import { spawn } from "node:child_process";

const TERM_PROGRAM = process.env.TERM_PROGRAM || "";
const CSM_BIN = process.env.CSM_BIN || "csm";

function shellQuote(s) {
    if (/^[A-Za-z0-9_\-./@,:=]+$/.test(s)) return s;
    return "'" + s.replace(/'/g, "'\\''") + "'";
}

function escAS(s) {
    return s.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function tryLaunch(extraArgs) {
    if (process.platform !== "darwin") return null;

    const cmd = [CSM_BIN, ...extraArgs].map(shellQuote).join(" ");

    if (TERM_PROGRAM === "ghostty") {
        const child = spawn("ghostty", ["-e", CSM_BIN, ...extraArgs], {
            detached: true,
            stdio: "ignore",
        });
        child.unref();
        return "Ghostty";
    }

    if (TERM_PROGRAM === "iTerm.app") {
        const script = `tell application "iTerm"\n    activate\n    create window with default profile command "${escAS(cmd)}"\nend tell`;
        const child = spawn("osascript", ["-e", script], { detached: true, stdio: "ignore" });
        child.unref();
        return "iTerm";
    }

    const script = `tell application "Terminal"\n    activate\n    do script "${escAS(cmd)}"\nend tell`;
    const child = spawn("osascript", ["-e", script], { detached: true, stdio: "ignore" });
    child.unref();
    return "Terminal";
}

const session = await joinSession({
    commands: [
        {
            name: "session-manager",
            description:
                "Browse, search, and resume Copilot CLI sessions in a TUI " +
                "(launches in a new terminal window).",
            handler: async (ctx) => {
                const args = (ctx.args || "").trim();
                const extra = args ? args.split(/\s+/) : [];
                const where = tryLaunch(extra);
                if (where) {
                    await session.log(
                        `Launched Copilot Session Manager${args ? ` with \`${args}\`` : ""} in a new ${where} window.`,
                    );
                } else {
                    await session.log(
                        `Run \`${CSM_BIN}${args ? " " + args : ""}\` in another terminal window.`,
                    );
                }
            },
        },
    ],
});
