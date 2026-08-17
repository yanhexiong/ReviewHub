"use client";

import { VscodeWorkspace } from "@/components/vscode-workspace";
import { useProjectIDFromLocation } from "@/lib/static-route";

export default function SourceEditorPage() {
  const projectId = useProjectIDFromLocation();
  if (!projectId) return null;
  return <VscodeWorkspace projectId={projectId} />;
}
