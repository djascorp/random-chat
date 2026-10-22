interface IconProps {
  size?: number;
  className?: string;
}

/** Inline stroke icons keep the bundle free of an icon dependency. */
function base(size: number, className: string | undefined, children: JSX.Element) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.7}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
      focusable="false"
    >
      {children}
    </svg>
  );
}

export const MicIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <rect x="9" y="3" width="6" height="11" rx="3" />
      <path d="M5 11a7 7 0 0 0 14 0" />
      <path d="M12 18v3" />
    </>,
  );

export const MicOffIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M15 9V6a3 3 0 0 0-5.9-.8" />
      <path d="M9 9v2a3 3 0 0 0 4.5 2.6" />
      <path d="M5 11a7 7 0 0 0 10.6 6" />
      <path d="M19 11a7 7 0 0 1-.6 2.8" />
      <path d="M12 18v3" />
      <path d="M3 3l18 18" />
    </>,
  );

export const VideoIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <rect x="2.5" y="6" width="13" height="12" rx="3" />
      <path d="M15.5 11l5-3v8l-5-3z" />
    </>,
  );

export const VideoOffIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M15.5 10.2V9a3 3 0 0 0-3-3H6.8" />
      <path d="M2.5 7.6V15a3 3 0 0 0 3 3h6a3 3 0 0 0 1.6-.5" />
      <path d="M20.5 8l-5 3v5l5 3z" />
      <path d="M3 3l18 18" />
    </>,
  );

export const SkipIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M5 5l9 7-9 7z" />
      <path d="M19 5v14" />
    </>,
  );

export const PhoneOffIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M3 10.5c5-5 13-5 18 0l-2.5 3-4-1.5v-2.5c-1.7-.7-3.3-.7-5 0V12L5.5 13.5z" />
      <path d="M4 20L20 4" />
    </>,
  );

export const ChatIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M20 12a7.5 7.5 0 0 1-7.5 7.5H9l-4 3v-4.4A7.5 7.5 0 0 1 12.5 4.5H13A7 7 0 0 1 20 11.5z" />
    </>,
  );

export const FlagIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M5 21V4" />
      <path d="M5 5h11l-1.5 4L16 13H5z" />
    </>,
  );

export const BlockIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M6 18L18 6" />
    </>,
  );

export const SlidersIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M5 7h14" />
      <path d="M5 12h14" />
      <path d="M5 17h14" />
      <circle cx="9" cy="7" r="1.8" />
      <circle cx="15" cy="12" r="1.8" />
      <circle cx="11" cy="17" r="1.8" />
    </>,
  );

export const SendIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M4.5 12L20 4.5 13 20l-2.5-6z" />
      <path d="M10.5 13.5L20 4.5" />
    </>,
  );

export const CloseIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M6 6l12 12" />
      <path d="M18 6L6 18" />
    </>,
  );

export const FlipCameraIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M4 9a8 8 0 0 1 13.6-4" />
      <path d="M20 15a8 8 0 0 1-13.6 4" />
      <path d="M18 3v4h-4" />
      <path d="M6 21v-4h4" />
    </>,
  );

export const ShieldIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M12 3l7 3v6c0 4.2-2.9 7.7-7 9-4.1-1.3-7-4.8-7-9V6z" />
      <path d="M9.5 12.2l1.8 1.8 3.4-3.6" />
    </>,
  );

export const UsersIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <circle cx="9" cy="8.5" r="3.5" />
      <path d="M2.5 20a6.5 6.5 0 0 1 13 0" />
      <path d="M16 5.2a3.5 3.5 0 0 1 0 6.6" />
      <path d="M17.5 20a6.6 6.6 0 0 0-1.5-4.2" />
    </>,
  );

export const BoltIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M13 2.5L5 13.5h5.5L10.5 21.5 19 10.5h-5.8z" />
    </>,
  );

export const GlobeIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17" />
      <path d="M12 3.5c2.5 2.4 3.8 5.4 3.8 8.5s-1.3 6.1-3.8 8.5c-2.5-2.4-3.8-5.4-3.8-8.5S9.5 5.9 12 3.5z" />
    </>,
  );

export const AlertIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M12 4l8.5 15H3.5z" />
      <path d="M12 9.5v4" />
      <path d="M12 16.5h.01" />
    </>,
  );

export const DotsIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <circle cx="5" cy="12" r="1" fill="currentColor" />
      <circle cx="12" cy="12" r="1" fill="currentColor" />
      <circle cx="19" cy="12" r="1" fill="currentColor" />
    </>,
  );

export const UserPlusIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <circle cx="9" cy="8.5" r="3.5" />
      <path d="M2.5 20a6.5 6.5 0 0 1 13 0" />
      <path d="M18.5 8v6" />
      <path d="M15.5 11h6" />
    </>,
  );

export const SparkIcon = ({ size = 20, className }: IconProps) =>
  base(
    size,
    className,
    <>
      <path d="M12 3v3" />
      <path d="M12 18v3" />
      <path d="M3 12h3" />
      <path d="M18 12h3" />
      <path d="M6.2 6.2l2.1 2.1" />
      <path d="M15.7 15.7l2.1 2.1" />
      <path d="M17.8 6.2l-2.1 2.1" />
      <path d="M8.3 15.7l-2.1 2.1" />
    </>,
  );
