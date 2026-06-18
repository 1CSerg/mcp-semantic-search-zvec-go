import React from "react";

/** REALWORLD_JSX_LEGACY renders a legacy-styled button. */
export function LegacyButton({ label, onClick }) {
  return (
    <button type="button" className="legacy-btn" onClick={onClick}>
      {label}
    </button>
  );
}
