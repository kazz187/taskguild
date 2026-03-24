import type { ComponentType, ReactNode, SVGProps } from 'react'

export interface PageHeadingProps {
  icon: ComponentType<SVGProps<SVGSVGElement>>
  title: string
  iconColor?: string
  children?: ReactNode
}

export function PageHeading({ icon: Icon, title, iconColor = 'text-cyan-400', children }: PageHeadingProps) {
  return (
    <div className="flex items-center gap-2">
      <Icon className={`w-5 h-5 ${iconColor}`} />
      <h2 className="text-lg md:text-xl font-bold text-white">{title}</h2>
      {children}
    </div>
  )
}
