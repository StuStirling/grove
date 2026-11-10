# Grove icon: macOS 26 appearances

macOS 26 takes Default, Dark, Clear and Tinted from one Icon Composer `.icon` file.
Build it from the layers in `icon-composer-layers/`, bottom to top:

1. `1-trunk.svg`
2. `2-crown.svg` (the branch lines and nodes are real holes, so Clear and Tinted show through them)
3. `3-active-node.svg` (the amber node)

The layers sit on a full-bleed 1024 canvas. Icon Composer applies the rounded-square mask itself.

## Colours per appearance

| Layer      | Default   | Dark      |
|------------|-----------|-----------|
| Background | `#1E3A2B` | `#0F1712` |
| Crown      | `#8CC97A` | `#7DBB6C` |
| Trunk      | `#F1ECDD` | `#CFC9BA` |
| Node       | `#E9C46A` | `#E9C46A` |

Clear and Tinted: leave the layers white or light, and let Icon Composer generate these appearances.
Previews of all four appearances are in `appearance-previews/`.

## Setting it up in Xcode

1. Add `Grove.icon` to the app target.
2. Set the target's App Icon to `Grove`.
3. Keep `AppIcon.appiconset` as the fallback for macOS 15 and earlier.
