//! Thin C bridge to Brave's adblock-rust. Builds WebKit/Safari content
//! blocking rules from standard filter lists (EasyList, EasyPrivacy, hosts).
//! The Go side owns the opaque handle and frees returned strings with
//! fasty_adblock_free_str.

use adblock::lists::{FilterFormat, FilterSet, ParseOptions, RuleTypes};
use std::ffi::{CStr, CString};
use std::os::raw::c_char;

pub struct FastyAdblock {
    set: FilterSet,
}

/// Create an empty, debug-mode filter set. Debug mode keeps the original
/// filter text, which into_content_blocking needs.
#[no_mangle]
pub extern "C" fn fasty_adblock_new() -> *mut FastyAdblock {
    Box::into_raw(Box::new(FastyAdblock {
        set: FilterSet::new(true),
    }))
}

/// Add a whole filter list. format: 0 = ABP/uBO rules, 1 = hosts file.
#[no_mangle]
pub unsafe extern "C" fn fasty_adblock_add_list(
    ab: *mut FastyAdblock,
    text: *const c_char,
    format: i32,
) {
    if ab.is_null() || text.is_null() {
        return;
    }
    let text = match CStr::from_ptr(text).to_str() {
        Ok(s) => s.to_owned(),
        Err(_) => return,
    };
    let opts = ParseOptions {
        format: if format == 1 {
            FilterFormat::Hosts
        } else {
            FilterFormat::Standard
        },
        rule_types: RuleTypes::NetworkOnly,
        ..ParseOptions::default()
    };
    (*ab).set.add_filter_list(text, opts);
}

/// Convert all added lists into a JSON array of WebKit content blocking
/// rules. Returns a malloc'd C string that must be freed with
/// fasty_adblock_free_str, or NULL on failure.
#[no_mangle]
pub unsafe extern "C" fn fasty_adblock_rules(ab: *const FastyAdblock) -> *mut c_char {
    if ab.is_null() {
        return std::ptr::null_mut();
    }
    let set = (*ab).set.clone();
    let (rules, _) = match set.into_content_blocking() {
        Ok(v) => v,
        Err(_) => return std::ptr::null_mut(),
    };
    let json = match serde_json::to_string(&rules) {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };
    match CString::new(json) {
        Ok(s) => s.into_raw(),
        Err(_) => std::ptr::null_mut(),
    }
}

/// Release a string returned by fasty_adblock_rules.
#[no_mangle]
pub unsafe extern "C" fn fasty_adblock_free_str(s: *mut c_char) {
    if !s.is_null() {
        drop(CString::from_raw(s));
    }
}

/// Release an engine handle.
#[no_mangle]
pub unsafe extern "C" fn fasty_adblock_free(ab: *mut FastyAdblock) {
    if !ab.is_null() {
        drop(Box::from_raw(ab));
    }
}
