import { useState, type ReactNode } from "react";
import { ObjectiveStatus, WorkspaceService, type WorkspaceSnapshot } from "@/lib/api";
import { useLiveWorkspace } from "@/features/workspace/use-workspace";
import { PencilIcon } from "lucide-react";
import { Markdown } from "@/components/markdown";
import { AutoTextarea } from "@/components/auto-textarea";
import { ErrorText } from "@/components/error-text";
import { ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";
import { usePending } from "@/hooks/use-pending";
import { WithPlanSources } from "@/features/workspace/plan-sources";
import { PREFERENCES_ID } from "@/features/workspace/preferences";
import { CurriculumTitle, ModuleItem, ModuleList, ModuleNote, ObjectiveItem, ObjectiveName, ObjectiveNote } from "@/features/workspace/curriculum-list";

type Props = {
  ws: WorkspaceSnapshot;
  onClose?: () => void;
  /** Opens the tutor beside the page, ready for a message about the plan. Not offered before the first plan. */
  onAskTutor?: () => void;
};

/** The plan's texts the learner edits by hand. */
type Doc = "mission" | "notes";

/**
 * The mission, how the learner likes to learn, and the curriculum on one
 * page, with the planning tutor in the chat beside it. There are two ways to
 * change them: edit the mission or the preferences yourself, or ask the
 * tutor, who proposes curriculum changes for you to review. The curriculum
 * isn't edited by hand.
 */
export function PlanView({ ws, onClose, onAskTutor }: Props) {
  const [editing, setEditing] = useState<Doc | null>(null);
  if (editing) return <DocEditor doc={editing} text={editing === "mission" ? ws.mission : ws.notes} onDone={() => setEditing(null)} />;

  const objectives = ws.progress.objectives ?? {};
  const passed = (id: string) => objectives[id]?.status === ObjectiveStatus.ObjectivePassed;
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
          <Button variant="outline" onClick={() => setEditing("mission")}>
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
      <WithPlanSources plan={ws.curriculum} resources={ws.resources}>
        {ws.mission.trim() ? (
          <Markdown text={ws.mission} className="prose" />
        ) : (
          <div className="prose">
            <h1>Mission</h1>
            <p className="text-muted-foreground">The tutor writes your mission after the first questions.</p>
          </div>
        )}
        <section id={PREFERENCES_ID} className="mt-9 scroll-mt-6">
          <div className="flex items-baseline justify-between gap-4">
            <CurriculumTitle>How you like to learn</CurriculumTitle>
            <Button variant="ghost" className="-mr-2" onClick={() => setEditing("notes")}>
              <PencilIcon className="size-3.5" /> Edit
            </Button>
          </div>
          {ws.notes.trim() ? (
            <Markdown text={ws.notes} className="prose mt-3" />
          ) : (
            <p className="mt-3 text-sm text-muted-foreground">
              Your tutor notes here how you like lessons and exercises written. Tell it after a lesson or exercise, or write it yourself.
            </p>
          )}
        </section>
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
      </WithPlanSources>
    </ReadingPage>
  );
}

const DOCS: Record<Doc, { title: string; hint: string; save: (wsId: string, text: string) => Promise<void> }> = {
  mission: {
    title: "Mission",
    hint: "Your tutor reads the new mission and suggests plan changes to match, if it needs any.",
    save: (wsId, text) => WorkspaceService.SaveMission(wsId, text),
  },
  notes: {
    title: "How you like to learn",
    hint: "Lessons and exercises written from now on follow what you write here.",
    save: (wsId, text) => WorkspaceService.SaveNotes(wsId, text),
  },
};

/** The mission or the preferences as plain text. Saving tells the tutor, who adjusts to match. */
function DocEditor({ doc, text: initial, onDone }: { doc: Doc; text: string; onDone: () => void }) {
  const { id: wsId } = useLiveWorkspace();
  const { title, hint, save: saveDoc } = DOCS[doc];
  const [text, setText] = useState(initial);
  const { pending: saving, error, run } = usePending();
  const save = () =>
    run(async () => {
      await saveDoc(wsId, text);
      onDone();
    });

  return (
    <ReadingPage>
      <PlanBar>
        <span className="text-sm text-muted-foreground">{hint}</span>
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
        <span className="text-[24px] leading-[1.2] font-bold">{title}</span>
        <AutoTextarea
          className="mt-2 min-h-40 px-4 py-3.5 font-sans text-[15px] leading-[1.65]"
          value={text}
          maxHeight={640}
          onChange={(e) => setText(e.target.value)}
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
