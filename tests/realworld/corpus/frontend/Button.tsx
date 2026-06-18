import React from "react";

/** REALWORLD_TSX_BUTTON primary action control. */
export interface ButtonProps {
  label: string;
  onClick?: () => void;
}

export function Button({ label, onClick }: ButtonProps) {
  return (
    <button type="button" className="btn-primary" onClick={onClick}>
      {label}
    </button>
  );
}
