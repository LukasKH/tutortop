import { useState, type ReactNode } from "react";
import { App, type WorkspaceSnapshot } from "@/lib/api";
import { PencilIcon } from "lucide-react";
import { Markdown } from "@/components/markdown";
import { AutoTextarea } from "@/components/auto-textarea";
import { ErrorText } from "@/components/error-text";
import { ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";
import { usePending } from "@/hooks/use-pending";
import { CurriculumTitle, ModuleItem, ModuleList, ModuleNote, ObjectiveItem, ObjectiveName, ObjectiveNote } from "@/features/workspace/curriculum-list";

type Props = {
  wsId: string;
  ws: WorkspaceSnapshot;
  onClose?: () => void;
  /** Opens the tutor beside the page, ready for a message about the plan. Not offered before the first plan. */
  onAskTutor?: () => void;
};

/**
 * The mission and the curriculum on one page, with the planning tutor in the
 * chat beside it. There are two ways to change them: edit the mission text
 * yourself, or ask the tutor, who proposes curriculum changes for you to
 * review. The curriculum isn't edited by hand.
 */
export function PlanView({ wsId, ws, onClose, onAskTutor }: Props) {
  const [editing, setEditing] = useState(false);
  if (editing) return <MissionEditor wsId={wsId} mission={ws.mission} onDone={() => setEditing(false)} />;

  const objectives = ws.progress.objectives ?? {};
  const passed = (id: string) => objectives[id]?.status === "passed";
  const modules = ws.curriculum?.modules ?? [];
  return (
    <ReadingPage>
      <PlanBar>
        {onClose ? (
          <Button variant="ghost" className="-ml-2" onClick={onClose}>
            ← Back
          </Button>
        ) : (
          <span />
        )}
        <div className="flex flex-none flex-wrap items-center gap-2">
          <Button variant="outline" onClick={() => setEditing(true)}>
            <PencilIcon className="size-3.5" /> Edit mission
          </Button>
          {onAskTutor && (
            <Button variant="outline" onClick={onAskTutor}>
              Change with the tutor
            </Button>
          )}
        </div>
      </PlanBar>
      <p className="-mt-1 mb-7 text-sm text-muted-foreground">
        {onAskTutor
          ? "Edit your mission yourself, or ask the tutor to change it or the curriculum. You review the tutor's changes before they apply."
          : "Edit your mission yourself. Your tutor builds the course plan from it."}
      </p>
      {ws.mission.trim() ? (
        <Markdown text={ws.mission} className="prose" />
      ) : (
        <div className="prose">
          <h1>Mission</h1>
          <p className="text-muted-foreground">The tutor writes your mission after the first questions.</p>
        </div>
      )}
      {ws.curriculum && (
        <section className="mt-9">
          <CurriculumTitle>Curriculum</CurriculumTitle>
          <ModuleList>
            {modules.map((m) => {
              const objs = m.objectives ?? [];
              return (
                <ModuleItem
                  key={m.id}
                  heading={
                    <>
                      {m.title}{" "}
                      <ModuleNote className="font-mono tabular-nums">
                        {objs.filter((o) => passed(o.id)).length}/{objs.length}
                      </ModuleNote>
                    </>
                  }
                >
                  {objs.map((o) => (
                    <ObjectiveItem key={o.id} done={passed(o.id)}>
                      <ObjectiveName>{o.name || o.title}</ObjectiveName>
                      {o.name && <ObjectiveNote>{o.title}</ObjectiveNote>}
                    </ObjectiveItem>
                  ))}
                </ModuleItem>
              );
            })}
          </ModuleList>
        </section>
      )}
    </ReadingPage>
  );
}

/** The mission as plain text. Saving tells the tutor, who proposes plan changes to match if needed. */
function MissionEditor({ wsId, mission: initial, onDone }: { wsId: string; mission: string; onDone: () => void }) {
  const [mission, setMission] = useState(initial);
  const { pending: saving, error, run } = usePending();
  const save = () =>
    run(async () => {
      await App.SaveMission(wsId, mission);
      onDone();
    });

  return (
    <ReadingPage>
      <PlanBar>
        <span className="text-sm text-muted-foreground">Your tutor reads the new mission and suggests plan changes to match, if it needs any.</span>
        <div className="flex flex-none flex-wrap items-center gap-2">
          <Button variant="ghost" onClick={onDone} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={save} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
      </PlanBar>
      <ErrorText error={error} className="mb-4" />
      <label className="flex min-w-0 flex-col gap-1.5">
        <span className="text-[24px] leading-[1.2] font-bold">Mission</span>
        <AutoTextarea
          className="mt-2 min-h-40 px-4 py-3.5 font-sans text-[15px] leading-[1.65]"
          value={mission}
          maxHeight={640}
          onChange={(e) => setMission(e.target.value)}
          autoFocus
        />
      </label>
    </ReadingPage>
  );
}

/** The page's top line: something on the left, its buttons on the right. */
function PlanBar({ children }: { children: ReactNode }) {
  return <div className="mb-4 flex items-center justify-between gap-4">{children}</div>;
}
