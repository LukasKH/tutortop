import { useEffect, useState, type ReactNode } from "react";
import { Clipboard, Dialogs } from "@wailsio/runtime";
import { CopilotService, errorText, type DeviceCode, type Plan, type Setup } from "@/lib/api";
import { checkEnvironment, useCheckingEnvironment, useEnvironment } from "@/lib/agents";
import { useTutor } from "@/features/settings/use-tutor";
import { useSettings } from "@/features/settings/use-settings";
import { TutorPicker } from "@/features/settings/tutor-picker";
import { AgentsSetting } from "@/features/settings/agents-setting";
import { AgentIcon } from "@/components/agent-icon";
import { ErrorText } from "@/components/error-text";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { GraduationCap, HardDrive, Sparkles } from "lucide-react";

/**
 * App settings in a modal, one page at a time from the sidebar: the tutor and
 * the agents it can run on, the editor's autocomplete, and where workspaces
 * are stored.
 */
export function SettingsDialog({ onClose }: { onClose: () => void }) {
  const { settings, change } = useSettings();
  const checkingEnv = useCheckingEnvironment();

  if (!settings) return null;
  return (
    // Closing unmounts the dialog. Escape and a click on the backdrop close it too.
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        showCloseButton={false}
        aria-describedby={undefined}
        // A fixed height, so switching pages doesn't make the dialog jump; only the page scrolls.
        className="flex h-[min(640px,calc(100vh-96px))] w-[min(760px,calc(100vw-48px))] max-w-none flex-col gap-0 overflow-hidden p-0 text-base sm:max-w-none"
      >
        <header className="flex flex-none items-center justify-between border-b px-5 pt-4 pb-3">
          <DialogTitle asChild>
            <h1 className="text-[20px] leading-[1.2] font-bold">Settings</h1>
          </DialogTitle>
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </header>

        {/* A sidebar of pages, or a row of tabs above the page in a narrow window. */}
        <Tabs defaultValue="tutor" orientation="vertical" className="min-h-0 flex-1 sm:flex-row">
          <TabsList className="h-auto gap-1 overflow-x-auto px-3 py-2 sm:w-44 sm:flex-col sm:items-stretch sm:gap-0.5 sm:border-r sm:border-b-0 sm:px-2 sm:py-3">
            {PAGES.map(({ id, label, Icon }) => (
              <TabsTrigger
                key={id}
                value={id}
                className="mb-0 justify-start gap-2 rounded-md border-b-0 px-2.5 py-1.5 text-base font-normal hover:bg-accent/60 data-active:bg-accent [&_svg]:text-muted-foreground data-active:[&_svg]:text-foreground"
              >
                <Icon size={16} aria-hidden="true" />
                {label}
              </TabsTrigger>
            ))}
          </TabsList>

          <TabsContent value="tutor" className={PAGE}>
            <div className={FIELD}>
              <span className={FIELD_LABEL}>Agent, model and reasoning</span>
              <TutorLine />
              <p className={FIELD_NOTE}>
                Changes apply from the tutor's next message. Switching agent starts new conversations; your mission, notes and progress stay.
              </p>
            </div>
            <div className={FIELD}>
              <div className="flex items-center justify-between gap-2">
                <span className={FIELD_LABEL}>Agents</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={checkingEnv}
                  onClick={() => checkEnvironment().catch(() => {})}
                  title="Check for newly installed agent CLIs"
                >
                  {checkingEnv && <Spinner />}
                  {checkingEnv ? "Updating…" : "Update"}
                </Button>
              </div>
              <AgentsSetting />
            </div>
          </TabsContent>

          <TabsContent value="autocomplete" className={PAGE}>
            <CopilotSetting enabled={settings.copilot} onChange={(copilot) => change({ ...settings, copilot })} />
          </TabsContent>

          <TabsContent value="storage" className={PAGE}>
            <WorkspacesFolder folder={settings.workspacesFolder ?? ""} onChange={(f) => change({ ...settings, workspacesFolder: f })} />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

/** The sidebar's pages, in order. */
const PAGES = [
  { id: "tutor", label: "Tutor", Icon: GraduationCap },
  { id: "autocomplete", label: "Autocomplete", Icon: Sparkles },
  { id: "storage", label: "Storage", Icon: HardDrive },
];

/** The configured tutor with a way to change it. */
export function TutorLine() {
  const tutor = useTutor();
  return (
    <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-soft-foreground">
      <span className="flex min-w-0 items-center gap-2" title={tutor.label}>
        {tutor.agent && <AgentIcon id={tutor.agent} size={14} />}
        <span className="min-w-0 truncate">{tutor.label || "…"}</span>
      </span>
      <TutorPicker align="end">
        <Button type="button" variant="ghost" className="flex-none">
          Change
        </Button>
      </TutorPicker>
    </div>
  );
}

/**
 * GitHub Copilot's suggestions in the editor, in three steps: install its
 * language server, sign in to GitHub, turn suggestions on.
 */
function CopilotSetting({ enabled, onChange }: { enabled: boolean; onChange: (on: boolean) => Promise<void> }) {
  const [setup, setSetup] = useState<Setup | null>(null);
  const [code, setCode] = useState<DeviceCode | null>(null);
  const [busy, setBusy] = useState(""); // what's going on, while a step runs
  const [error, setError] = useState("");

  useEffect(() => {
    CopilotService.Setup()
      .then(setSetup)
      .catch((e) => setError(errorText(e)));
  }, []);

  /** Runs one step, showing what it's doing, then the setup it leaves. */
  async function step(doing: string, work: () => Promise<Setup | null>) {
    setBusy(doing);
    setError("");
    try {
      const next = await work();
      if (next) setSetup(next);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }

  const install = () => step("Downloading Copilot, about 120 MB…", () => CopilotService.Install());
  const check = () => step("Checking…", () => CopilotService.Setup());
  const signIn = () =>
    step("Starting sign-in…", async () => {
      const c = await CopilotService.SignIn();
      if (!c.userCode) return CopilotService.Setup(); // signed in already, e.g. in another editor
      setCode(c);
      return null;
    });
  // Copilot opens GitHub's page; the code is on the clipboard to paste there.
  // The address is shown too, for when no browser opens (server mode).
  const finishSignIn = ({ userCode, verificationUri }: DeviceCode) =>
    step(`Waiting for you to paste the code ${userCode} at ${verificationUri?.replace(/^https?:\/\//, "") || "GitHub"}…`, async () => {
      // Best-effort: the code stays visible on screen if clipboard access fails.
      await Clipboard.SetText(userCode).catch(() => {});
      try {
        return await CopilotService.FinishSignIn();
      } finally {
        setCode(null);
      }
    });
  function toggle(on: boolean) {
    setError("");
    onChange(on).catch((e) => setError(errorText(e)));
  }
  const signOut = () =>
    step("Signing out…", async () => {
      await CopilotService.SignOut();
      return CopilotService.Setup();
    });

  return (
    <div className={FIELD}>
      <span className={FIELD_LABEL}>GitHub Copilot</span>
      {!setup ? (
        busy ? null : error ? (
          <SettingRow text="Couldn't check Copilot">
            <Button type="button" variant="ghost" onClick={check}>
              Check again
            </Button>
            <Button type="button" variant="ghost" onClick={install}>
              Reinstall
            </Button>
          </SettingRow>
        ) : (
          <p className="text-muted-foreground">Loading…</p>
        )
      ) : !setup.installed ? (
        <SettingRow text="Not installed">
          <Button type="button" variant="ghost" disabled={!!busy} onClick={install}>
            Install
          </Button>
        </SettingRow>
      ) : code ? (
        <SettingRow text="Sign in on GitHub with a one-time code">
          <Button type="button" variant="ghost" disabled={!!busy} onClick={() => finishSignIn(code)}>
            Copy code and open GitHub
          </Button>
        </SettingRow>
      ) : !setup.user && !setup.problem ? (
        <SettingRow text="Not signed in">
          <Button type="button" variant="ghost" disabled={!!busy} onClick={signIn}>
            Sign in with GitHub
          </Button>
        </SettingRow>
      ) : (
        <>
          {setup.user && (
            <SettingRow text={[`Signed in as ${setup.user}`, setup.plan?.name].filter(Boolean).join(" · ")}>
              <Button type="button" variant="ghost" disabled={!!busy} onClick={signOut}>
                Sign out
              </Button>
            </SettingRow>
          )}
          {setup.problem ? (
            <SettingRow text={<span className="text-fail">{setup.problem}</span>}>
              <Button type="button" variant="ghost" disabled={!!busy} onClick={check}>
                Check again
              </Button>
            </SettingRow>
          ) : (
            <Label className="cursor-pointer gap-2.5 py-[5px] text-base leading-normal font-normal text-soft-foreground has-[[data-state=checked]]:text-foreground">
              <Checkbox checked={enabled} onCheckedChange={(on) => toggle(on === true)} />
              Suggest the rest of the line as you type
            </Label>
          )}
        </>
      )}
      {busy && (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Spinner />
          {busy}
        </p>
      )}
      <ErrorText error={error} />
      <p className={FIELD_NOTE}>
        Suggestions only finish the line you're typing, so you still write the solution yourself. They send the file you're editing to GitHub.{" "}
        {allowance(setup?.plan)}
        {setup?.installed && (
          <>
            {" "}
            Copilot {setup.version}.{/* Only when GitHub has a newer release: an update re-downloads about 120 MB. */}
            {setup.update && (
              <>
                {" "}
                <Button type="button" variant="link" size="sm" className="h-auto px-0 text-xs" disabled={!!busy} onClick={install}>
                  Update to {setup.update}
                </Button>
              </>
            )}
          </>
        )}
      </p>
    </div>
  );
}

/** How many suggestions the plan allows: the plan's own numbers once Copilot has said, else the rule. */
function allowance(plan: Plan | null | undefined): string {
  if (plan?.unlimited) return `${plan.name} has no limit on suggestions.`;
  if (plan?.allowance) {
    const left = Math.max(0, plan.allowance - (plan.used ?? 0)).toLocaleString();
    const parsed = plan.resetDate ? new Date(plan.resetDate) : null;
    const until = parsed && Number.isFinite(parsed.getTime()) ? `, until ${parsed.toLocaleDateString(undefined, { month: "long", day: "numeric" })}` : "";
    return `${plan.name}: ${left} of ${plan.allowance.toLocaleString()} suggestions left this month${until}.`;
  }
  return "Copilot Free includes 2,000 suggestions a month; paid plans have no limit.";
}

/** A line of grey text with its action on the right. */
function SettingRow({ text, children }: { text: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-soft-foreground">
      <span className="min-w-0">{text}</span>
      <span className="flex flex-none gap-1">{children}</span>
    </div>
  );
}

/**
 * Where workspaces live. The app opens workspaces by name while it runs, so a
 * new folder is used from the next start.
 */
function WorkspacesFolder({ folder, onChange }: { folder: string; onChange: (folder: string) => Promise<void> }) {
  const env = useEnvironment();
  const [error, setError] = useState("");
  if (!env) return <p className="text-muted-foreground">Loading…</p>;
  const shown = env.rootFromEnv ? env.root : folder || env.defaultRoot;
  const pending = !env.rootFromEnv && shown !== env.root;

  function save(next: string) {
    setError("");
    onChange(next).catch((e) => setError(errorText(e)));
  }

  function pick() {
    // The system's own folder picker. Not available in server mode (a browser).
    Dialogs.OpenFile({
      Title: "Choose where to keep workspaces",
      ButtonText: "Use this folder",
      Directory: shown,
      CanChooseDirectories: true,
      CanChooseFiles: false,
      CanCreateDirectories: true,
    })
      .then((path) => path && save(path))
      .catch((e) => setError(errorText(e)));
  }

  return (
    <div className={FIELD}>
      <span className={FIELD_LABEL}>Workspaces folder</span>
      <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-soft-foreground">
        <code className="min-w-0 truncate" title={shown}>
          {shown}
        </code>
        {!env.rootFromEnv && (
          <span className="flex flex-none gap-1">
            {folder && (
              <Button type="button" variant="ghost" onClick={() => save("")}>
                Use default
              </Button>
            )}
            <Button type="button" variant="ghost" onClick={pick}>
              Change…
            </Button>
          </span>
        )}
      </div>
      <ErrorText error={error} />
      <p className={FIELD_NOTE}>
        {env.rootFromEnv ? (
          <>Set by the TUTORTOP_HOME environment variable, which overrides this setting.</>
        ) : pending ? (
          <>
            Tutortop switches to this folder the next time it starts. Workspaces in <code>{env.root}</code> stay where they are; move their folders over to keep
            them.
          </>
        ) : (
          <>
            Each workspace is a folder of plain files with its own git history. Hidden tests and reference solutions are kept elsewhere, so the tutor can't see
            them.
          </>
        )}
      </p>
    </div>
  );
}

// A page's fields, scrolling on their own under the fixed header.
const PAGE = "flex min-h-0 flex-col gap-[18px] overflow-auto px-5 pt-4 pb-5";

const FIELD = "flex min-w-0 flex-col gap-1.5";

const FIELD_LABEL = "text-sm text-muted-foreground";

const FIELD_NOTE = "text-xs text-muted-foreground";
