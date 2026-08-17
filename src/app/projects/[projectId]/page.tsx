"use client";

import { ProjectHome } from "@/components/project-home";
import { useProjectIDFromLocation } from "@/lib/static-route";

export default function ProjectPage() {
  const projectId = useProjectIDFromLocation();
  if (!projectId) return null;
  return <ProjectHome projectId={projectId} />;
}
