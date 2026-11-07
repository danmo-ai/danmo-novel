fn main() {
    let target = std::env::var("TARGET").unwrap_or_default();
    if target.contains("apple-ios") {
        let root = std::path::PathBuf::from(std::env::var_os("CARGO_MANIFEST_DIR").unwrap());
        let slice = if target.ends_with("-sim") {
            "ios-arm64_x86_64-simulator"
        } else {
            "ios-arm64"
        };
        println!(
            "cargo:rustc-link-search=framework={}/ios/Frameworks/DanmoNovel.xcframework/{}",
            root.display(),
            slice
        );
        println!("cargo:rustc-link-lib=framework=DanmoNovel");
        println!("cargo:rerun-if-changed=ios/Frameworks/DanmoNovel.xcframework");
    }
    tauri_plugin::Builder::new(&["bootstrap", "request"])
        .android_path("android")
        .ios_path("ios")
        .build();
}
