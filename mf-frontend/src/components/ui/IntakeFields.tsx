"use client";

import type { ReactElement } from "react";

export function IntakeFields(props: {
  topic: string;
  purpose: string;
  onTopic: (v: string) => void;
  onPurpose: (v: string) => void;
  disabled?: boolean;
}): ReactElement {
  const { topic, purpose, onTopic, onPurpose, disabled } = props;

  return (
    <div className="well p-3 space-y-3">
      <div>
        <label className="label" htmlFor="persona-konu">
          Konu
        </label>
        <input
          id="persona-konu"
          className="input"
          value={topic}
          onChange={(e) => onTopic(e.target.value)}
          disabled={disabled}
          placeholder="Pazar, marka, ürün veya teknoloji"
          autoComplete="off"
        />
      </div>
      <div>
        <label className="label" htmlFor="persona-amac">
          Amaç
        </label>
        <input
          id="persona-amac"
          className="input"
          value={purpose}
          onChange={(e) => onPurpose(e.target.value)}
          disabled={disabled}
          placeholder="Ne için değerlendiriyorsun?"
          autoComplete="off"
        />
      </div>
    </div>
  );
}
