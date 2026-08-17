"use client";

import { useEffect, useState } from "react";

function decodeSegment(value: string | undefined) {
  if (!value) return "";
  try {
    const decoded = decodeURIComponent(value);
    return decoded.includes("/") ? "" : decoded;
  } catch {
    return "";
  }
}

export function useProjectIDFromLocation() {
  const [projectID, setProjectID] = useState("");

  useEffect(() => {
    const match = window.location.pathname.match(
      /^\/projects\/([^/]+)(?:\/|$)/,
    );
    setProjectID(decodeSegment(match?.[1]));
  }, []);

  return projectID;
}

export function useShareTokenFromLocation() {
  const [token, setToken] = useState("");

  useEffect(() => {
    const match = window.location.pathname.match(/^\/share\/([^/]+)(?:\/|$)/);
    setToken(decodeSegment(match?.[1]));
  }, []);

  return token;
}

export function useSnapshotIDFromLocation() {
  const [snapshotID, setSnapshotID] = useState<string | undefined>();

  useEffect(() => {
    setSnapshotID(
      new URLSearchParams(window.location.search).get("snapshotId") ??
        undefined,
    );
  }, []);

  return snapshotID;
}
